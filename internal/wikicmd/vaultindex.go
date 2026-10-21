package wikicmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"wiki_skill/internal/vault"
)

const (
	vaultIndexFile = "_index.md"
	overviewFile   = "_overview.md"
)

// buckets are the top-level PARA folders that hold Project, Area and Resource folders.
var buckets = []string{"projects", "areas", "resources"}

type folder struct {
	Kind string // bucket name: projects, areas or resources
	Name string
	Dir  string // absolute path
}

func listFolders(root string) []folder {
	var folders []folder
	for _, kind := range buckets {
		entries, err := os.ReadDir(filepath.Join(root, kind))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				folders = append(folders, folder{Kind: kind, Name: e.Name(), Dir: filepath.Join(root, kind, e.Name())})
			}
		}
	}
	return folders
}

// folderNotes returns the absolute paths of a folder's notes, sorted. Files
// starting with "_" (the generated vault index and the overview) are not notes.
func folderNotes(dir string) ([]string, error) {
	var notes []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") || strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		notes = append(notes, path)
		return nil
	})
	sort.Strings(notes)
	return notes, err
}

func noteStem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

func noteTitle(path string) string {
	note, err := vault.ReadNote(path)
	if err == nil {
		if title, _ := note.Meta["title"].(string); title != "" {
			return title
		}
	}
	return noteStem(path)
}

func isVaultIndex(path string) bool {
	return filepath.Base(path) == vaultIndexFile
}

func isGeneratedOrOverview(path string) bool {
	return isVaultIndex(path) || filepath.Base(path) == overviewFile
}

// writeVaultIndex regenerates a folder's _index.md (links only, never hand-edited), skipping the write when nothing changed.
func writeVaultIndex(f folder) error {
	notes, err := folderNotes(f.Dir)
	if err != nil {
		return err
	}
	var body strings.Builder
	for _, p := range notes {
		fmt.Fprintf(&body, "- [[%s]]: %s\n", noteStem(p), noteTitle(p))
	}
	meta := map[string]interface{}{
		"title": f.Name + " Index",
		"type":  "index",
	}
	rendered, err := vault.RenderFrontmatter(meta, body.String())
	if err != nil {
		return err
	}
	path := filepath.Join(f.Dir, vaultIndexFile)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == rendered {
		return nil // unchanged: keep the file's mtime, and no needless editor/sync churn
	}
	return os.WriteFile(path, []byte(rendered), 0o644)
}

// WriteVaultIndexes regenerates every folder's _index.md.
func WriteVaultIndexes(root string) error {
	for _, f := range listFolders(root) {
		if err := writeVaultIndex(f); err != nil {
			return err
		}
	}
	return nil
}

func withoutVaultIndexes(paths []string) []string {
	var kept []string
	for _, p := range paths {
		if !isVaultIndex(p) {
			kept = append(kept, p)
		}
	}
	return kept
}

// refreshVaultIndexes regenerates the _index.md of every folder that holds a
// changed or deleted note. Both slices hold absolute file paths.
func refreshVaultIndexes(root string, changed, deleted []string) error {
	affected := map[string]bool{}
	for _, p := range append(append([]string{}, changed...), deleted...) {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 3 {
			continue
		}
		affected[filepath.Join(root, parts[0], parts[1])] = true
	}
	for _, f := range listFolders(root) {
		if affected[f.Dir] {
			if err := writeVaultIndex(f); err != nil {
				return err
			}
		}
	}
	return nil
}
