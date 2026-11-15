package wikicmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"wiki_skill/internal/embedworker"
	"wiki_skill/internal/index"
	"wiki_skill/internal/ranking"
	"wiki_skill/internal/vault"
)

// SearchResult is one note, merged from the keyword and the semantic ranking.
type SearchResult = ranking.Result

type GrepMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchReport struct {
	Query           string         `json:"query"`
	Results         []SearchResult `json:"results"`
	GrepMatches     []GrepMatch    `json:"grep_matches"`
	SemanticMatches []index.Result `json:"semantic_matches"`
	SemanticIndexed bool           `json:"semantic_indexed"`
	// Warning is set when the semantic ranking was skipped (the embedder is down, the index does not match the
	// embedding model): Results are then by keywords alone.
	Warning string `json:"warning,omitempty"`
}

// ReindexReport says how many notes the index holds, how many were (re)embedded and how many were dropped because
// their file is gone. Full is true for a rebuild from scratch.
type ReindexReport struct {
	NotesIndexed int  `json:"notes_indexed"`
	Embedded     int  `json:"embedded"`
	Removed      int  `json:"removed"`
	Full         bool `json:"full"`
	// Failed lists the notes that could not be read (they are left out; the rest was indexed).
	Failed []string `json:"failed,omitempty"`
	DBPath string   `json:"db_path"`
}

type MergeCandidate struct {
	PathA      string  `json:"path_a"`
	PathB      string  `json:"path_b"`
	Similarity float64 `json:"similarity"`
}

type StaleFolder struct {
	Bucket   string  `json:"bucket"`
	Name     string  `json:"name"`
	DaysIdle float64 `json:"days_idle"`
}

type PruneReport struct {
	MergeCandidates []MergeCandidate `json:"merge_candidates"`
	StaleFolders    []StaleFolder    `json:"stale_folders"`
}

func noteToIndexText(root, path string) (index.NoteText, error) {
	note, err := vault.ReadNote(path)
	if err != nil {
		return index.NoteText{}, err
	}
	title := note.Title()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return index.NoteText{}, err
	}
	text := strings.TrimSpace(title + "\n" + note.Body)
	return index.NoteText{Path: rel, Text: text}, nil
}

// Reindex brings the search index up to date with the vault. By default it is incremental: only notes whose text
// changed since they were indexed are embedded again, and notes whose file is gone are dropped. full rebuilds the
// whole index from scratch (needed after changing the embedding model), keeping the old index if that fails.
func Reindex(root string, embed embedworker.Embedder, full bool) (ReindexReport, error) {
	dbPath := index.Path(root)
	db, err := index.Open(root)
	if err != nil {
		return ReindexReport{}, err
	}
	defer db.Close()

	if err := WriteVaultIndexes(root); err != nil {
		return ReindexReport{}, err
	}

	paths, err := vault.IterNotes(root)
	if err != nil {
		return ReindexReport{}, err
	}

	if full {
		var notes []index.NoteText
		for _, p := range paths {
			nt, err := noteToIndexText(root, p)
			if err != nil {
				return ReindexReport{}, err
			}
			notes = append(notes, nt)
		}
		if err := index.RebuildIndex(db, notes, embed); err != nil {
			return ReindexReport{}, err
		}
		return ReindexReport{NotesIndexed: len(notes), Embedded: len(notes), Full: true, DBPath: dbPath}, nil
	}

	// Incremental: the same step `wiki watch` takes, for every note on disk and every indexed note whose file is gone
	// (the vault indexes were just written above).
	onDisk := make(map[string]bool, len(paths))
	for _, p := range paths {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return ReindexReport{}, err
		}
		onDisk[rel] = true
	}
	indexed, err := index.IndexedPaths(db)
	if err != nil {
		return ReindexReport{}, err
	}
	var gone []string
	for _, rel := range indexed {
		if !onDisk[rel] {
			gone = append(gone, filepath.Join(root, rel))
		}
	}
	out, err := apply(root, paths, gone, embed, false)
	if err != nil {
		return ReindexReport{}, err
	}
	report := ReindexReport{NotesIndexed: len(paths) - len(out.Failed), Embedded: len(out.Upserted), Removed: len(out.Removed), DBPath: dbPath}
	for _, f := range out.Failed {
		report.Failed = append(report.Failed, fmt.Sprintf("%s: %v", f.Path, f.Err))
	}
	return report, nil
}

// Search ranks notes three ways, by the query's words (BM25 over the notes, file names and titles weighted; BM25 over
// sections, for a fact on one line of a long note) and by meaning (the best section's embedding), and merges them
// into Results, one per note with the section that matches
// best. GrepMatches (the query as a literal string, as before) and SemanticMatches stay for older callers.
func Search(root, query string, embed embedworker.Embedder, topK int) (SearchReport, error) {
	report := SearchReport{Query: query}

	// -F: the query is text, not a regex ("(tokens per second)" is not a group)
	cmd := exec.Command("rg", "--line-number", "--no-heading", "-i", "-F", "--", query, root)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run() // rg exits non-zero when there are no matches; that's not an error here

	for _, line := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		lineNum, convErr := strconv.Atoi(parts[1])
		if convErr != nil {
			continue
		}
		file := parts[0]
		if rel, relErr := filepath.Rel(root, file); relErr == nil {
			file = rel
		}
		report.GrepMatches = append(report.GrepMatches, GrepMatch{File: file, Line: lineNum, Text: parts[2]})
	}
	if len(report.GrepMatches) > 20 {
		report.GrepMatches = report.GrepMatches[:20]
	}

	notes, err := searchNotes(root)
	if err != nil {
		return SearchReport{}, err
	}

	if _, err := os.Stat(index.Path(root)); err == nil {
		report.SemanticIndexed = true
		matches, err := semanticMatches(root, query, embed, len(notes)+1) // every note: each candidate gets a similarity
		if err != nil {
			report.Warning = fmt.Sprintf("semantic ranking skipped, results are by keywords only: %v", err)
		}
		report.SemanticMatches = matches
	}

	report.Results = ranking.Rank(notes, query, report.SemanticMatches, topK)
	if len(report.SemanticMatches) > topK {
		report.SemanticMatches = report.SemanticMatches[:topK]
	}
	return report, nil
}

func semanticMatches(root, query string, embed embedworker.Embedder, n int) ([]index.Result, error) {
	db, err := index.Open(root)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return index.Search(db, query, embed, n)
}

// searchNotes reads every note for the ranking (generated vault indexes are not notes).
func searchNotes(root string) ([]ranking.Note, error) {
	paths, err := vault.IterNotes(root)
	if err != nil {
		return nil, err
	}
	var notes []ranking.Note
	for _, p := range paths {
		note, err := vault.ReadNote(p)
		if err != nil {
			continue // a note with broken frontmatter is skipped, not fatal for search
		}
		rel, _ := filepath.Rel(root, p)
		notes = append(notes, ranking.Note{Path: rel, Title: note.Title(), Date: metaString(note.Meta["date"]),
			Tags: metaString(note.Meta["tags"]), Body: note.Body})
	}
	return notes, nil
}

func metaString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case time.Time:
		return t.Format("2006-01-02")
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = fmt.Sprint(x)
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(t)
	}
}

func Prune(root string, similarityThreshold float64, staleDays float64) (PruneReport, error) {
	var report PruneReport
	if _, statErr := os.Stat(index.Path(root)); statErr == nil {
		db, err := index.Open(root)
		if err != nil {
			return PruneReport{}, err
		}
		pairs, err := index.NearDuplicates(db, similarityThreshold)
		db.Close()
		if err != nil {
			return PruneReport{}, err
		}
		for _, p := range pairs {
			report.MergeCandidates = append(report.MergeCandidates, MergeCandidate{PathA: p.PathA, PathB: p.PathB, Similarity: p.Similarity})
		}
	}

	now := time.Now()
	for _, bucket := range []string{"projects", "areas"} {
		bucketDir := filepath.Join(root, bucket)
		entries, err := os.ReadDir(bucketDir)
		if err != nil {
			continue
		}
		var folderNames []string
		for _, e := range entries {
			if e.IsDir() {
				folderNames = append(folderNames, e.Name())
			}
		}
		sort.Strings(folderNames)
		for _, name := range folderNames {
			folder := filepath.Join(bucketDir, name)
			var newest time.Time
			found := false
			vault.Walk(folder, folder, func(p string, kind vault.Kind) error {
				if kind == vault.KindGenerated {
					return nil
				}
				info, infoErr := os.Stat(p)
				if infoErr != nil {
					return nil
				}
				if info.ModTime().After(newest) {
					newest = info.ModTime()
				}
				found = true
				return nil
			})
			if !found {
				continue
			}
			ageDays := now.Sub(newest).Hours() / 24
			if ageDays >= staleDays {
				report.StaleFolders = append(report.StaleFolders, StaleFolder{Bucket: bucket, Name: name, DaysIdle: ageDays})
			}
		}
	}

	return report, nil
}
