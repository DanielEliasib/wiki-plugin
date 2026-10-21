package wikicmd

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed templates/conventions.md
var conventionsTemplate []byte

// vaultDirs are the folders a new vault starts with.
var vaultDirs = []string{"projects", "areas", "resources", "archive", "drafts", ".wiki"}

type InitReport struct {
	Root    string   `json:"root"`
	Created []string `json:"created"`
}

// Init creates the vault's folders and .wiki/conventions.md under root. It
// only adds what is missing, so running it on an existing vault changes nothing.
func Init(root string) (InitReport, error) {
	report := InitReport{Root: root, Created: []string{}}
	for _, dir := range vaultDirs {
		path := filepath.Join(root, dir)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return report, err
		}
		report.Created = append(report.Created, dir+"/")
	}
	conventions := filepath.Join(root, ".wiki", "conventions.md")
	if _, err := os.Stat(conventions); err != nil {
		if err := os.WriteFile(conventions, conventionsTemplate, 0o644); err != nil {
			return report, err
		}
		report.Created = append(report.Created, ".wiki/conventions.md")
	}
	return report, nil
}
