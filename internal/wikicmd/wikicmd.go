package wikicmd

import (
	"bytes"
	"database/sql"
	"io/fs"
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
	"wiki_skill/internal/vault"
)

type GrepMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchReport struct {
	Query           string         `json:"query"`
	GrepMatches     []GrepMatch    `json:"grep_matches"`
	SemanticMatches []index.Result `json:"semantic_matches"`
	SemanticIndexed bool           `json:"semantic_indexed"`
}

type ReindexReport struct {
	NotesIndexed int    `json:"notes_indexed"`
	DBPath       string `json:"db_path"`
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

func openIndexDB(root string) (*sql.DB, string, error) {
	dbPath := filepath.Join(root, ".wiki", "index.sqlite")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(10000)")
	return db, dbPath, err
}

func noteToIndexText(root, path string) (index.NoteText, error) {
	note, err := vault.ReadNote(path)
	if err != nil {
		return index.NoteText{}, err
	}
	title, _ := note.Meta["title"].(string)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return index.NoteText{}, err
	}
	text := strings.TrimSpace(title + "\n" + note.Body)
	return index.NoteText{Path: rel, Text: text}, nil
}

func Reindex(root string, embed embedworker.Embedder) (ReindexReport, error) {
	dbDir := filepath.Join(root, ".wiki")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return ReindexReport{}, err
	}
	db, dbPath, err := openIndexDB(root)
	if err != nil {
		return ReindexReport{}, err
	}
	defer db.Close()
	if err := index.InitDB(db); err != nil {
		return ReindexReport{}, err
	}

	if err := WriteVaultIndexes(root); err != nil {
		return ReindexReport{}, err
	}

	paths, err := vault.IterNotes(root)
	if err != nil {
		return ReindexReport{}, err
	}

	var notes []index.NoteText
	for _, p := range paths {
		if isVaultIndex(p) {
			continue
		}
		nt, err := noteToIndexText(root, p)
		if err != nil {
			return ReindexReport{}, err
		}
		notes = append(notes, nt)
	}

	if err := index.RebuildIndex(db, notes, embed); err != nil {
		return ReindexReport{}, err
	}
	return ReindexReport{NotesIndexed: len(notes), DBPath: dbPath}, nil
}

// ProcessBatch upserts the still-existing paths in changed (hash-filtered,
// embedded in one batched call) and removes the paths in deleted. Both
// slices hold absolute file paths. It returns the vault-relative paths that
// were actually upserted and removed, for callers that report on activity
// (e.g. wiki watch --json).
func ProcessBatch(root string, db *sql.DB, changed, deleted []string, embed embedworker.Embedder) (upserted, removed []string, err error) {
	changed = withoutVaultIndexes(changed)
	deleted = withoutVaultIndexes(deleted)
	if err := refreshVaultIndexes(root, changed, deleted); err != nil {
		return nil, nil, err
	}

	for _, path := range deleted {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil, nil, relErr
		}
		if err := index.RemoveNote(db, rel); err != nil {
			return nil, nil, err
		}
		removed = append(removed, rel)
	}

	var notes []index.NoteText
	for _, path := range changed {
		nt, ntErr := noteToIndexText(root, path)
		if ntErr != nil {
			return nil, nil, ntErr
		}
		notes = append(notes, nt)
	}
	if len(notes) == 0 {
		return nil, removed, nil
	}
	upserted, err = index.UpsertBatch(db, notes, embed)
	if err != nil {
		return nil, nil, err
	}
	return upserted, removed, nil
}

func Search(root, query string, embed embedworker.Embedder, topK int) (SearchReport, error) {
	report := SearchReport{Query: query}

	cmd := exec.Command("rg", "--line-number", "--no-heading", "-i", query, root)
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

	dbPath := filepath.Join(root, ".wiki", "index.sqlite")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return report, nil
	}
	report.SemanticIndexed = true

	db, _, err := openIndexDB(root)
	if err != nil {
		return SearchReport{}, err
	}
	defer db.Close()

	results, err := index.Search(db, query, embed, topK)
	if err != nil {
		return SearchReport{}, err
	}
	report.SemanticMatches = results
	return report, nil
}

func Prune(root string, similarityThreshold float64, staleDays float64) (PruneReport, error) {
	var report PruneReport
	dbPath := filepath.Join(root, ".wiki", "index.sqlite")
	if _, statErr := os.Stat(dbPath); statErr == nil {
		db, _, err := openIndexDB(root)
		if err != nil {
			return PruneReport{}, err
		}
		rows, err := db.Query("SELECT path, embedding FROM notes")
		if err != nil {
			db.Close()
			return PruneReport{}, err
		}
		type vecEntry struct {
			path string
			vec  []float64
		}
		var entries []vecEntry
		for rows.Next() {
			var p string
			var blob []byte
			if err := rows.Scan(&p, &blob); err != nil {
				rows.Close()
				db.Close()
				return PruneReport{}, err
			}
			entries = append(entries, vecEntry{path: p, vec: index.FromBlob(blob)})
		}
		rows.Close()
		db.Close()

		for i := 0; i < len(entries); i++ {
			for j := i + 1; j < len(entries); j++ {
				sim := index.Cosine(entries[i].vec, entries[j].vec)
				if sim >= similarityThreshold {
					report.MergeCandidates = append(report.MergeCandidates, MergeCandidate{
						PathA: entries[i].path, PathB: entries[j].path, Similarity: sim,
					})
				}
			}
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
			filepath.WalkDir(folder, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") || isVaultIndex(p) {
					return nil
				}
				info, infoErr := d.Info()
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
