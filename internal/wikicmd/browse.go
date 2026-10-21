package wikicmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"wiki_skill/internal/vault"
)

type FolderSummary struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Gist      string `json:"gist"`
	NoteCount int    `json:"note_count"`
}

type NoteSummary struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// IndexReport is the result of Browse: every folder when no name was given,
// otherwise the notes of the one named folder.
type IndexReport struct {
	Folders []FolderSummary `json:"folders,omitempty"`
	Folder  string          `json:"folder,omitempty"`
	Notes   []NoteSummary   `json:"notes,omitempty"`
}

// overview is the parsed, optional _overview.md of a folder.
type overview struct {
	Title   string
	Summary string
	Paths   []string
}

func readOverview(f folder) overview {
	note, err := vault.ReadNote(filepath.Join(f.Dir, overviewFile))
	if err != nil {
		return overview{}
	}
	var o overview
	o.Title, _ = note.Meta["title"].(string)
	o.Summary, _ = note.Meta["summary"].(string)
	if list, ok := note.Meta["paths"].([]interface{}); ok {
		for _, p := range list {
			if s, ok := p.(string); ok && s != "" {
				o.Paths = append(o.Paths, s)
			}
		}
	}
	return o
}

// gist is the folder's one-line description: the overview's summary, falling
// back to its title.
func (o overview) gist() string {
	if o.Summary != "" {
		return strings.Join(strings.Fields(o.Summary), " ")
	}
	return o.Title
}

// Index browses the vault top-down, read live from the notes themselves:
// all folders with descriptions, or with a name (or kind/name when the same
// name exists in two buckets), that folder's notes.
func Index(root, name string) (IndexReport, error) {
	folders := listFolders(root)
	if name == "" {
		var report IndexReport
		for _, f := range folders {
			notes, err := folderNotes(f.Dir)
			if err != nil {
				return IndexReport{}, err
			}
			report.Folders = append(report.Folders, FolderSummary{
				Kind: f.Kind, Name: f.Name, Gist: readOverview(f).gist(), NoteCount: len(notes),
			})
		}
		return report, nil
	}
	var matches []folder
	for _, f := range folders {
		if f.Name == name || f.Kind+"/"+f.Name == name {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 0:
	case 1:
		f := matches[0]
		notes, err := folderNotes(f.Dir)
		if err != nil {
			return IndexReport{}, err
		}
		report := IndexReport{Folder: f.Name}
		for _, p := range notes {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return IndexReport{}, err
			}
			report.Notes = append(report.Notes, NoteSummary{Path: rel, Title: noteTitle(p)})
		}
		return report, nil
	default:
		var names []string
		for _, f := range matches {
			names = append(names, f.Kind+"/"+f.Name)
		}
		return IndexReport{}, fmt.Errorf("%q is ambiguous; use one of: %s", name, strings.Join(names, ", "))
	}
	return IndexReport{}, fmt.Errorf("no folder named %q in the vault", name)
}
