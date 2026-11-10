package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"wiki_skill/internal/embedworker"
)

func InitDB(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS notes (
			path TEXT PRIMARY KEY,
			text TEXT NOT NULL,
			hash TEXT NOT NULL,
			embedding BLOB NOT NULL
		)
	`); err != nil {
		return err
	}

	rows, err := db.Query("PRAGMA table_info(notes)")
	if err != nil {
		return err
	}
	hasHash := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "hash" {
			hasHash = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	if !hasHash {
		if _, err := db.Exec("DROP TABLE notes"); err != nil {
			return err
		}
		if _, err := db.Exec(`
			CREATE TABLE notes (
				path TEXT PRIMARY KEY,
				text TEXT NOT NULL,
				hash TEXT NOT NULL,
				embedding BLOB NOT NULL
			)
		`); err != nil {
			return err
		}
	}

	// One row per section of a note (chunks.go). An index from before chunks has notes but no chunks: their hashes
	// are cleared so the next reindex or watch pass embeds them again, by section.
	var hasChunks int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'chunks'").Scan(&hasChunks); err != nil {
		return err
	}
	if hasChunks == 0 {
		if _, err := db.Exec(`
			CREATE TABLE chunks (
				path TEXT NOT NULL,
				idx INTEGER NOT NULL,
				heading TEXT NOT NULL,
				text TEXT NOT NULL,
				embedding BLOB NOT NULL,
				PRIMARY KEY (path, idx)
			)
		`); err != nil {
			return err
		}
		if _, err := db.Exec("UPDATE notes SET hash = ''"); err != nil {
			return err
		}
	}
	return nil
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// embedded is a note with its whole-note vector and its chunks' vectors.
type embedded struct {
	path, text, hash string
	vec              []float64
	chunks           []Chunk
	chunkVecs        [][]float64
}

// embedNotes embeds every note whole and by chunk, in one call to the embedder.
func embedNotes(notes []NoteText, embed embedworker.Embedder) ([]embedded, error) {
	out := make([]embedded, len(notes))
	var texts []string
	for i, n := range notes {
		out[i] = embedded{path: n.Path, text: n.Text, hash: HashText(n.Text), chunks: SplitChunks(n.Text)}
		texts = append(texts, n.Text)
		for _, c := range out[i].chunks {
			texts = append(texts, chunkEmbedText(titleOf(n.Text), c))
		}
	}
	if len(texts) == 0 {
		return out, nil
	}
	vectors, err := embed(texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d texts", len(vectors), len(texts))
	}
	k := 0
	for i := range out {
		out[i].vec = vectors[k]
		k++
		out[i].chunkVecs = vectors[k : k+len(out[i].chunks)]
		k += len(out[i].chunks)
	}
	return out, nil
}

// writeNote stores a note's row and replaces its chunks.
func writeNote(db execer, e embedded) error {
	if _, err := db.Exec(`
		INSERT INTO notes (path, text, hash, embedding) VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET text = excluded.text, hash = excluded.hash, embedding = excluded.embedding
	`, e.path, e.text, e.hash, ToBlob(e.vec)); err != nil {
		return err
	}
	if _, err := db.Exec("DELETE FROM chunks WHERE path = ?", e.path); err != nil {
		return err
	}
	for i, c := range e.chunks {
		if _, err := db.Exec("INSERT INTO chunks (path, idx, heading, text, embedding) VALUES (?, ?, ?, ?, ?)",
			e.path, i, c.Heading, c.Text, ToBlob(e.chunkVecs[i])); err != nil {
			return err
		}
	}
	return nil
}

func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func HashForPath(db *sql.DB, path string) (hash string, found bool, err error) {
	err = db.QueryRow("SELECT hash FROM notes WHERE path = ?", path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

func ToBlob(vec []float64) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(float32(v)))
	}
	return buf
}

func FromBlob(blob []byte) []float64 {
	n := len(blob) / 4
	vec := make([]float64, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(blob[i*4:])
		vec[i] = float64(math.Float32frombits(bits))
	}
	return vec
}

func Cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	denom := math.Sqrt(na) * math.Sqrt(nb)
	if denom == 0 {
		denom = 1
	}
	return dot / denom
}

func UpsertNote(db *sql.DB, path, text string, embed embedworker.Embedder) error {
	hash := HashText(text)
	existingHash, found, err := HashForPath(db, path)
	if err != nil {
		return err
	}
	if found && existingHash == hash {
		return nil
	}

	e, err := embedNotes([]NoteText{{Path: path, Text: text}}, embed)
	if err != nil {
		return err
	}
	return writeNote(db, e[0])
}

func RemoveNote(db *sql.DB, path string) error {
	if _, err := db.Exec("DELETE FROM chunks WHERE path = ?", path); err != nil {
		return err
	}
	_, err := db.Exec("DELETE FROM notes WHERE path = ?", path)
	return err
}

// Result is one note: its best-matching chunk's score, heading and text.
type Result struct {
	Path    string  `json:"path"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
	Heading string  `json:"heading,omitempty"`
	Excerpt string  `json:"excerpt,omitempty"`
}

// Search ranks notes by their best chunk's similarity to the query, one result per note. Notes without chunks (an
// index from before chunks, not yet re-embedded) are scored by their whole-note vector.
func Search(db *sql.DB, query string, embed embedworker.Embedder, topK int) ([]Result, error) {
	vectors, err := embed([]string{query})
	if err != nil {
		return nil, err
	}
	queryVec := vectors[0]
	best := map[string]Result{}
	consider := func(r Result) {
		if cur, ok := best[r.Path]; !ok || r.Score > cur.Score {
			best[r.Path] = r
		}
	}

	rows, err := db.Query("SELECT path, heading, text, embedding FROM chunks")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var path, heading, text string
		var blob []byte
		if err := rows.Scan(&path, &heading, &text, &blob); err != nil {
			rows.Close()
			return nil, err
		}
		consider(Result{Path: path, Score: Cosine(queryVec, FromBlob(blob)), Snippet: snippet(text), Heading: heading, Excerpt: text})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	rows, err = db.Query("SELECT path, text, embedding FROM notes WHERE path NOT IN (SELECT DISTINCT path FROM chunks)")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var path, text string
		var blob []byte
		if err := rows.Scan(&path, &text, &blob); err != nil {
			rows.Close()
			return nil, err
		}
		consider(Result{Path: path, Score: Cosine(queryVec, FromBlob(blob)), Snippet: snippet(text)})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(best))
	for _, r := range best {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func snippet(text string) string {
	if len(text) > 160 {
		cut := 160
		for cut > 0 && (text[cut]&0xC0) == 0x80 {
			cut--
		}
		text = text[:cut]
	}
	return strings.ReplaceAll(text, "\n", " ")
}

type NoteText struct {
	Path string
	Text string
}

// RebuildIndex replaces the whole index with notes. Every note is embedded
// first; the old rows are only replaced, in one transaction, once all vectors
// are in hand, so a failed or interrupted rebuild leaves the old index intact.
func RebuildIndex(db *sql.DB, notes []NoteText, embed embedworker.Embedder) error {
	all, err := embedNotes(notes, embed)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.Exec("DELETE FROM notes"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM chunks"); err != nil {
		return err
	}
	for _, e := range all {
		if err := writeNote(tx, e); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func UpsertBatch(db *sql.DB, notes []NoteText, embed embedworker.Embedder) ([]string, error) {
	type pending struct {
		path string
		text string
		hash string
	}

	var toEmbed []pending
	for _, n := range notes {
		hash := HashText(n.Text)
		existingHash, found, err := HashForPath(db, n.Path)
		if err != nil {
			return nil, err
		}
		if found && existingHash == hash {
			continue
		}
		toEmbed = append(toEmbed, pending{path: n.Path, text: n.Text, hash: hash})
	}
	if len(toEmbed) == 0 {
		return nil, nil
	}

	pendingNotes := make([]NoteText, len(toEmbed))
	for i, p := range toEmbed {
		pendingNotes[i] = NoteText{Path: p.path, Text: p.text}
	}
	all, err := embedNotes(pendingNotes, embed)
	if err != nil {
		return nil, err
	}

	upserted := make([]string, 0, len(all))
	for _, e := range all {
		if err := writeNote(db, e); err != nil {
			return nil, err
		}
		upserted = append(upserted, e.path)
	}
	return upserted, nil
}
