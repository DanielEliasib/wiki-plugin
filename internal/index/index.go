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

	vectors, err := embed([]string{text})
	if err != nil {
		return err
	}
	blob := ToBlob(vectors[0])
	_, err = db.Exec(`
		INSERT INTO notes (path, text, hash, embedding) VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET text = excluded.text, hash = excluded.hash, embedding = excluded.embedding
	`, path, text, hash, blob)
	return err
}

func RemoveNote(db *sql.DB, path string) error {
	_, err := db.Exec("DELETE FROM notes WHERE path = ?", path)
	return err
}

type Result struct {
	Path    string  `json:"path"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

func Search(db *sql.DB, query string, embed embedworker.Embedder, topK int) ([]Result, error) {
	vectors, err := embed([]string{query})
	if err != nil {
		return nil, err
	}
	queryVec := vectors[0]

	rows, err := db.Query("SELECT path, text, embedding FROM notes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var path, text string
		var blob []byte
		if err := rows.Scan(&path, &text, &blob); err != nil {
			return nil, err
		}
		score := Cosine(queryVec, FromBlob(blob))
		snippet := text
		if len(snippet) > 160 {
			snippet = snippet[:160]
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		results = append(results, Result{Path: path, Score: score, Snippet: snippet})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

type NoteText struct {
	Path string
	Text string
}

// RebuildIndex replaces the whole index with notes. Every note is embedded
// first; the old rows are only replaced, in one transaction, once all vectors
// are in hand, so a failed or interrupted rebuild leaves the old index intact.
func RebuildIndex(db *sql.DB, notes []NoteText, embed embedworker.Embedder) error {
	var vectors [][]float64
	if len(notes) > 0 {
		texts := make([]string, len(notes))
		for i, n := range notes {
			texts[i] = n.Text
		}
		var err error
		if vectors, err = embed(texts); err != nil {
			return err
		}
		if len(vectors) != len(notes) {
			return fmt.Errorf("embedder returned %d vectors for %d notes", len(vectors), len(notes))
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.Exec("DELETE FROM notes"); err != nil {
		return err
	}
	for i, n := range notes {
		_, err := tx.Exec(`
			INSERT INTO notes (path, text, hash, embedding) VALUES (?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET text = excluded.text, hash = excluded.hash, embedding = excluded.embedding
		`, n.Path, n.Text, HashText(n.Text), ToBlob(vectors[i]))
		if err != nil {
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

	texts := make([]string, len(toEmbed))
	for i, p := range toEmbed {
		texts[i] = p.text
	}
	vectors, err := embed(texts)
	if err != nil {
		return nil, err
	}

	upserted := make([]string, 0, len(toEmbed))
	for i, p := range toEmbed {
		blob := ToBlob(vectors[i])
		_, err := db.Exec(`
			INSERT INTO notes (path, text, hash, embedding) VALUES (?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET text = excluded.text, hash = excluded.hash, embedding = excluded.embedding
		`, p.path, p.text, p.hash, blob)
		if err != nil {
			return nil, err
		}
		upserted = append(upserted, p.path)
	}
	return upserted, nil
}
