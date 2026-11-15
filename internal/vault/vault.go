package vault

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"wiki_skill/internal/config"
)

type Note struct {
	Path string
	Meta map[string]interface{}
	Body string
}

// Title is the frontmatter title, or "" when there is none.
func (n Note) Title() string {
	title, _ := n.Meta["title"].(string)
	return title
}

var frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?\n)---\n(.*)$`)

func expandHome(p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}

func ResolveVaultRoot(flagValue string) (string, error) {
	if flagValue != "" {
		return expandHome(flagValue)
	}
	if env := os.Getenv("WIKI_ROOT"); env != "" {
		return expandHome(env)
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if cfg.Vault != "" {
		return expandHome(cfg.Vault)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "notes", "wiki"), nil
}

func ParseFrontmatter(text string) (map[string]interface{}, string, error) {
	match := frontmatterRe.FindStringSubmatch(text)
	if match == nil {
		return map[string]interface{}{}, text, nil
	}
	var meta map[string]interface{}
	if err := yaml.Unmarshal([]byte(match[1]), &meta); err != nil {
		return nil, "", err
	}
	if meta == nil {
		meta = map[string]interface{}{}
	}
	return meta, match[2], nil
}

func RenderFrontmatter(meta map[string]interface{}, body string) (string, error) {
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		keyNode := &yaml.Node{}
		if err := keyNode.Encode(k); err != nil {
			return "", err
		}
		valNode := &yaml.Node{}
		if err := valNode.Encode(meta[k]); err != nil {
			return "", err
		}
		node.Content = append(node.Content, keyNode, valNode)
	}

	out, err := yaml.Marshal(node)
	if err != nil {
		return "", err
	}
	header := strings.TrimRight(string(out), "\n")
	return fmt.Sprintf("---\n%s\n---\n%s", header, body), nil
}

func ReadNote(path string) (Note, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Note{}, err
	}
	meta, body, err := ParseFrontmatter(string(data))
	if err != nil {
		return Note{}, err
	}
	return Note{Path: path, Meta: meta, Body: body}, nil
}

func WriteNote(path string, meta map[string]interface{}, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rendered, err := RenderFrontmatter(meta, body)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(rendered), 0o644)
}

// Kind says what a path in the Vault is.
type Kind int

const (
	KindNone      Kind = iota // not a Note: another file type, or inside a hidden folder
	KindNote                  // a Note
	KindOverview              // a project, area or resource's _overview.md: a Note, flagged
	KindGenerated             // a generated _index.md (Vault index): never a Note
)

const (
	overviewFile  = "_overview.md"
	generatedFile = "_index.md"
)

// IsHiddenName reports whether a file or folder name is hidden. Hidden folders (.wiki's own data, .obsidian,
// .claude/worktrees copies of the Vault) hold no Notes.
func IsHiddenName(name string) bool {
	return strings.HasPrefix(name, ".")
}

// Classify decides what a Vault-relative path is. It looks only at the path.
func Classify(rel string) Kind {
	if !strings.HasSuffix(rel, ".md") {
		return KindNone
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if IsHiddenName(part) {
			return KindNone
		}
	}
	switch filepath.Base(rel) {
	case generatedFile:
		return KindGenerated
	case overviewFile:
		return KindOverview
	}
	return KindNote
}

// Walk calls fn, in lexical order, for every Note, overview and generated index under dir (a folder inside root),
// skipping hidden folders and other files.
func Walk(root, dir string, fn func(path string, kind Kind) error) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && IsHiddenName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if kind := Classify(rel); kind != KindNone {
			return fn(path, kind)
		}
		return nil
	})
}

// IterNotes lists the absolute paths of every Note and overview in the Vault, sorted. Generated indexes are left out.
func IterNotes(root string) ([]string, error) {
	var notes []string
	err := Walk(root, root, func(path string, kind Kind) error {
		if kind != KindGenerated {
			notes = append(notes, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(notes)
	return notes, nil
}
