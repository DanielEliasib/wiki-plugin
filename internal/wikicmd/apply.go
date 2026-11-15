package wikicmd

import (
	"path/filepath"

	"wiki_skill/internal/embedworker"
	"wiki_skill/internal/index"
)

// Failure is one Note that could not be applied, with the reason.
type Failure struct {
	Path string // absolute
	Err  error
}

// Outcome says what Apply did. Paths in Upserted and Removed are Vault-relative.
type Outcome struct {
	Upserted []string
	Removed  []string
	Failed   []Failure
	// VaultIndexErr is set when a generated _index.md could not be written; the Notes were still applied.
	VaultIndexErr error
}

// Apply brings the Search index and the Vault indexes in line with a set of changed and deleted files (absolute
// paths): the one step both `wiki watch` and `wiki reindex` use. Changed paths that are not Notes are ignored. A
// Note that cannot be read goes into Outcome.Failed and the others are applied anyway; an error means the whole
// batch failed (the embedder or the index) and every path may be tried again.
func Apply(root string, changed, deleted []string, embed embedworker.Embedder) (Outcome, error) {
	unlock, err := index.Lock(root)
	if err != nil {
		return Outcome{}, err
	}
	defer unlock()
	return apply(root, changed, deleted, embed, true)
}

func apply(root string, changed, deleted []string, embed embedworker.Embedder, refreshIndexes bool) (Outcome, error) {
	var out Outcome
	changed = onlyNotes(root, changed)
	if len(changed) == 0 && len(deleted) == 0 {
		return out, nil
	}
	if refreshIndexes {
		out.VaultIndexErr = refreshVaultIndexes(root, changed, deleted)
	}

	db, err := index.Open(root)
	if err != nil {
		return out, err
	}
	defer db.Close()

	// Removing a path that was never indexed is harmless, so deleted paths are not filtered: this is also how an
	// index shed what an older version stored from a hidden folder.
	for _, path := range deleted {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return out, err
		}
		if err := index.RemoveNote(db, rel); err != nil {
			return out, err
		}
		out.Removed = append(out.Removed, rel)
	}

	var notes []index.NoteText
	for _, path := range changed {
		nt, err := noteToIndexText(root, path)
		if err != nil {
			out.Failed = append(out.Failed, Failure{Path: path, Err: err})
			continue
		}
		notes = append(notes, nt)
	}
	if len(notes) == 0 {
		return out, nil
	}
	out.Upserted, err = index.UpsertBatch(db, notes, embed)
	return out, err
}
