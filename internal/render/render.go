package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"wiki_skill/internal/wikicmd"
)

func encodeJSON(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// truncate shortens s to at most max runes, appending an ellipsis when it
// had to cut. Snippets stay long enough to be useful but short enough not
// to wrap awkwardly in a normal terminal width.
func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max])) + "…"
}

// styles holds a restrained set of roles — one accent color for the
// numbers/labels that matter, bold for identifiers, faint for supporting
// text — bound to a specific writer so color auto-disables for non-TTY
// output (pipes, redirects, tests).
type styles struct {
	header lipgloss.Style
	accent lipgloss.Style
	path   lipgloss.Style
	dim    lipgloss.Style
	errLbl lipgloss.Style
}

func newStyles(w io.Writer) styles {
	r := lipgloss.NewRenderer(w)
	return styles{
		header: r.NewStyle().Bold(true).Foreground(lipgloss.Color("4")),
		accent: r.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		path:   r.NewStyle().Bold(true),
		dim:    r.NewStyle().Faint(true),
		errLbl: r.NewStyle().Bold(true).Foreground(lipgloss.Color("1")),
	}
}

func Search(w io.Writer, jsonMode bool, r wikicmd.SearchReport) error {
	if jsonMode {
		return encodeJSON(w, r)
	}
	s := newStyles(w)

	fmt.Fprintln(w, s.header.Render("Results (best first; match: keyword, semantic or both)"))
	fmt.Fprintln(w)
	if len(r.Results) == 0 {
		fmt.Fprintln(w, s.dim.Render("  (no matches)"))
	}
	for i, res := range r.Results {
		if i > 0 {
			fmt.Fprintln(w)
		}
		title := res.Title
		if res.Date != "" && !strings.Contains(title, res.Date) {
			title += " (" + res.Date + ")"
		}
		how := res.Match
		if res.Score > 0 {
			how += fmt.Sprintf(", similarity %.2f", res.Score)
		}
		fmt.Fprintf(w, "  %s  %s\n", s.path.Render(res.Path), s.dim.Render(how))
		if title != "" {
			fmt.Fprintf(w, "    %s\n", title)
		}
		excerpt := strings.Join(strings.Fields(res.Excerpt), " ")
		if res.Heading != "" && !strings.HasPrefix(strings.TrimLeft(res.Excerpt, "# "), res.Heading) {
			excerpt = "§ " + res.Heading + ": " + excerpt
		}
		fmt.Fprintf(w, "    %s\n", s.dim.Render(truncate(excerpt, 200)))
	}
	if !r.SemanticIndexed {
		fmt.Fprintln(w)
		fmt.Fprintln(w, s.dim.Render("  (keyword matches only: no semantic index yet — run reindex)"))
	}
	if len(r.GrepMatches) == 0 {
		return nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, s.header.Render("Exact phrase matches"))
	fmt.Fprintln(w)
	{
		for i, m := range r.GrepMatches {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "  %s  %s\n", s.path.Render(m.File), s.dim.Render(fmt.Sprintf("line %d", m.Line)))
			fmt.Fprintf(w, "         %s\n", s.dim.Render(truncate(m.Text, 90)))
		}
	}

	return nil
}

func Reindex(w io.Writer, jsonMode bool, r wikicmd.ReindexReport) error {
	if jsonMode {
		return encodeJSON(w, r)
	}
	s := newStyles(w)
	fmt.Fprintf(w, "Reindexed %s notes into %s\n",
		s.accent.Render(fmt.Sprintf("%d", r.NotesIndexed)),
		s.dim.Render(r.DBPath),
	)
	return nil
}

func Prune(w io.Writer, jsonMode bool, r wikicmd.PruneReport) error {
	if jsonMode {
		return encodeJSON(w, r)
	}
	s := newStyles(w)

	fmt.Fprintln(w, s.header.Render("Merge candidates"))
	fmt.Fprintln(w)
	if len(r.MergeCandidates) == 0 {
		fmt.Fprintln(w, s.dim.Render("  (none)"))
	} else {
		for _, c := range r.MergeCandidates {
			fmt.Fprintf(w, "  %s  %s  %s  %s\n",
				s.accent.Render(fmt.Sprintf("%.3f", c.Similarity)),
				s.path.Render(c.PathA),
				s.dim.Render("<->"),
				s.path.Render(c.PathB),
			)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, s.header.Render("Stale folders"))
	fmt.Fprintln(w)
	if len(r.StaleFolders) == 0 {
		fmt.Fprintln(w, s.dim.Render("  (none)"))
	} else {
		for _, sf := range r.StaleFolders {
			fmt.Fprintf(w, "  %s  %s\n",
				s.path.Render(fmt.Sprintf("%s/%s", sf.Bucket, sf.Name)),
				s.dim.Render(fmt.Sprintf("(%.0f days idle)", sf.DaysIdle)),
			)
		}
	}
	return nil
}

func Error(w io.Writer, jsonMode bool, err error) {
	if jsonMode {
		_ = encodeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	s := newStyles(w)
	fmt.Fprintln(w, s.errLbl.Render("error:"), err)
}

func Index(w io.Writer, jsonMode bool, r wikicmd.IndexReport) error {
	if jsonMode {
		return encodeJSON(w, r)
	}
	s := newStyles(w)

	if r.Folder != "" {
		fmt.Fprintln(w, s.header.Render(r.Folder))
		fmt.Fprintln(w)
		for _, n := range r.Notes {
			fmt.Fprintf(w, "  %s  %s\n", s.accent.Render(n.Title), s.path.Render(n.Path))
		}
		return nil
	}
	if len(r.Folders) == 0 {
		fmt.Fprintln(w, s.dim.Render("(no folders)"))
		return nil
	}
	for _, f := range r.Folders {
		line := fmt.Sprintf("  %s %s", s.accent.Render(f.Name), s.dim.Render(fmt.Sprintf("(%s, %d)", f.Kind, f.NoteCount)))
		if f.Gist != "" {
			line += "  " + f.Gist
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

func Init(w io.Writer, jsonMode bool, r wikicmd.InitReport) error {
	if jsonMode {
		return encodeJSON(w, r)
	}
	s := newStyles(w)
	if len(r.Created) == 0 {
		fmt.Fprintf(w, "Vault at %s is already set up.\n", s.path.Render(r.Root))
		return nil
	}
	fmt.Fprintf(w, "Initialized vault at %s\n", s.path.Render(r.Root))
	for _, c := range r.Created {
		fmt.Fprintf(w, "  %s %s\n", s.accent.Render("+"), c)
	}
	return nil
}
