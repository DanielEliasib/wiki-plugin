package wikicmd

import (
	"fmt"
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
	err := vault.Walk(dir, dir, func(path string, kind vault.Kind) error {
		if kind == vault.KindNote && !strings.HasPrefix(filepath.Base(path), "_") {
			notes = append(notes, path)
		}
		return nil
	})
	sort.Strings(notes)
	return notes, err
}

func noteStem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

func noteTitle(path string) string {
	if note, err := vault.ReadNote(path); err == nil {
		if title := note.Title(); title != "" {
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
	for i, g := range groupBySubfolder(f.Dir, notes) {
		if g.name != "" {
			if i > 0 {
				body.WriteString("\n")
			}
			fmt.Fprintf(&body, "## %s\n\n", g.name)
		}
		for _, p := range g.paths {
			fmt.Fprintf(&body, "- [[%s]]: %s\n", noteStem(p), noteTitle(p))
		}
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

type indexGroup struct {
	name  string // "" for notes/ itself (and notes at the folder's top level), else the kind or folder: "decisions", "logs"
	paths []string
}

// groupBySubfolder splits a folder's notes by the subfolder they sit in, for the vault index: the plain notes first,
// then each kind under notes/ (decisions, questions, specs ...) in name order, then drafts/, logs/ and the rest.
func groupBySubfolder(dir string, notes []string) []indexGroup {
	byName := map[string][]string{}
	for _, p := range notes {
		rel, err := filepath.Rel(dir, filepath.Dir(p))
		if err != nil {
			rel = "."
		}
		rel = filepath.ToSlash(rel)
		name := strings.TrimPrefix(rel, "notes/")
		if rel == "." || rel == "notes" {
			name = ""
		}
		byName[name] = append(byName[name], p)
	}
	rank := func(name string) int {
		switch {
		case name == "":
			return 0
		case !strings.Contains(name, "/") && name != "drafts" && name != "logs" && name != "raw":
			return 1 // a kind under notes/ (rel was notes/<kind>)
		default:
			return 2
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if ri, rj := rank(names[i]), rank(names[j]); ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	groups := make([]indexGroup, 0, len(names))
	for _, n := range names {
		groups = append(groups, indexGroup{name: n, paths: byName[n]})
	}
	return groups
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

// onlyNotes keeps the absolute paths that are Notes (or overviews) of the Vault: no generated indexes, no hidden
// folders, no other file types.
func onlyNotes(root string, paths []string) []string {
	var kept []string
	for _, p := range paths {
		if isNotePath(root, p) {
			kept = append(kept, p)
		}
	}
	return kept
}

func isNotePath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	kind := vault.Classify(rel)
	return kind == vault.KindNote || kind == vault.KindOverview
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
