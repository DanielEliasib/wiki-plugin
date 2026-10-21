package wikicmd

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"wiki_skill/internal/vault"
)

// mapCharBudget caps the Session map at about 750 tokens (4 characters each).
const mapCharBudget = 750 * 4

// Weights of the relevance score that picks the Session map's notes:
//
//	score = 1.0·ln(1+backlinks) + 1.0·0.5^(age/30d) + 4.0·[note in a cwd-matched folder]
//
// (from the weights prototype: the large cwd bonus puts a matching folder's
// notes on top; with no match, hubs mix with fresh notes.)
const (
	weightBacklinks = 1.0
	weightRecency   = 1.0
	weightCwd       = 4.0
	recencyHalfLife = 30 * 24 * time.Hour
	maxGistChars    = 110
)

var backlinkRe = regexp.MustCompile(`\[\[([^\]|#]+)`)

type mapNote struct {
	Path      string // vault-relative
	Title     string
	Pinned    bool
	Backlinks int
	ModTime   time.Time
	Score     float64
}

// cwdMatches reports whether cwd equals or is inside any of paths, compared by
// path component (so ~/a/ecu does not match ~/a/ecu-tools).
func cwdMatches(cwd string, paths []string) bool {
	cwd = cleanPath(cwd)
	for _, p := range paths {
		p = cleanPath(p)
		if cwd == p || strings.HasPrefix(cwd, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func cleanPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return filepath.Clean(p)
}

// countBacklinks counts [[wikilinks]] across the vault by target basename.
// Links from the generated _index.md and from _overview.md files are ignored,
// as are links from a note to itself.
func countBacklinks(root string) (map[string]int, error) {
	paths, err := vault.IterNotes(root)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range paths {
		if isGeneratedOrOverview(p) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		self := noteStem(p)
		for _, m := range backlinkRe.FindAllStringSubmatch(string(data), -1) {
			target := strings.TrimSuffix(filepath.Base(strings.TrimSpace(m[1])), ".md")
			if target != self {
				counts[target]++
			}
		}
	}
	return counts, nil
}

// Map builds the Session map: a nudge line, every Project, Area and Resource
// with its gist and note count, then pinned notes followed by the highest
// scoring notes, within the token budget. It reads the vault live and never
// writes. now is passed in so the recency decay is testable.
func Map(root, cwd string, now time.Time) (string, error) {
	backlinks, err := countBacklinks(root)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	fmt.Fprintf(&out, "The wiki vault at %s holds notes from past work: decisions, research, setup.\n", root)
	out.WriteString("Earlier decisions and research on these topics are findable with `wiki search <query>` (or the wiki skill); searching before researching or deciding anything avoids redoing them.\n")

	var notes []mapNote
	folders := listFolders(root)
	for _, kind := range buckets {
		header := false
		for _, f := range folders {
			if f.Kind != kind {
				continue
			}
			paths, err := folderNotes(f.Dir)
			if err != nil {
				return "", err
			}
			ov := readOverview(f)
			matched := cwdMatches(cwd, ov.Paths)

			if !header {
				fmt.Fprintf(&out, "\n%s:\n", strings.ToUpper(kind[:1])+kind[1:])
				header = true
			}
			line := fmt.Sprintf("- %s (%d)", f.Name, len(paths))
			if g := truncateRunes(ov.gist(), maxGistChars); g != "" {
				line += ": " + g
			}
			if matched {
				line += " ← cwd"
			}
			out.WriteString(line + "\n")

			for _, p := range paths {
				note, err := vault.ReadNote(p)
				if err != nil {
					continue // one unreadable note must not blank the whole map
				}
				info, err := os.Stat(p)
				if err != nil {
					return "", err
				}
				rel, err := filepath.Rel(root, p)
				if err != nil {
					return "", err
				}
				n := mapNote{Path: rel, Title: noteTitle(p), Backlinks: backlinks[noteStem(p)], ModTime: info.ModTime()}
				n.Pinned, _ = note.Meta["pinned"].(bool)
				age := now.Sub(n.ModTime)
				if age < 0 {
					age = 0
				}
				n.Score = weightBacklinks*math.Log1p(float64(n.Backlinks)) +
					weightRecency*math.Pow(0.5, float64(age)/float64(recencyHalfLife))
				if matched {
					n.Score += weightCwd
				}
				notes = append(notes, n)
			}
		}
	}

	sort.SliceStable(notes, func(i, j int) bool {
		a, b := notes[i], notes[j]
		switch {
		case a.Pinned != b.Pinned:
			return a.Pinned
		case a.Score != b.Score:
			return a.Score > b.Score
		case a.Backlinks != b.Backlinks:
			return a.Backlinks > b.Backlinks
		case !a.ModTime.Equal(b.ModTime):
			return a.ModTime.After(b.ModTime)
		}
		return a.Path < b.Path
	})

	if len(notes) > 0 {
		heading := "\nNotes:\n"
		used := out.Len() + len(heading)
		var lines []string
		for _, n := range notes {
			line := fmt.Sprintf("- %s [%s]\n", n.Title, n.Path)
			if used+len(line) > mapCharBudget {
				break
			}
			used += len(line)
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			out.WriteString(heading)
			for _, l := range lines {
				out.WriteString(l)
			}
		}
	}
	return out.String(), nil
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
