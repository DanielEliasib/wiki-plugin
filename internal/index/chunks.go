package index

import (
	"regexp"
	"strings"
)

// A note is embedded twice: whole (the notes table, for prune's similarity between notes) and by section (the
// chunks table, for search). One vector per note has two problems for search: the embedder keeps only the first
// 512 tokens, so a fact further down a long note is invisible, and a note about several things gets a blurred
// vector that matches none of them well. A chunk is one section (heading plus text), split further when long, and
// is embedded with the note's title and the heading so it keeps its context.

// ChunkMaxChars keeps a chunk well under the embedder's 512 tokens (about 4 characters per token in English).
const ChunkMaxChars = 1200

// chunkMinChars: a section shorter than this is merged into the next one, or the one before when it is last (a
// heading with one line says little).
const chunkMinChars = 200

type Chunk struct {
	Heading string // the section's heading text ("" before the first heading)
	Text    string // the section text, heading line included
}

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)

// SplitChunks splits a note's index text (title line, then body; see noteToIndexText) into chunks.
func SplitChunks(text string) []Chunk {
	lines := strings.Split(text, "\n")
	if len(lines) > 0 {
		lines = lines[1:] // the title: it goes in front of every chunk's embedded text instead
	}
	type section struct {
		heading string
		lines   []string
	}
	var sections []section
	cur := section{}
	fence := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
		}
		if m := headingRe.FindStringSubmatch(l); m != nil && !fence {
			if len(cur.lines) > 0 {
				sections = append(sections, cur)
			}
			cur = section{heading: m[2], lines: []string{l}}
			continue
		}
		cur.lines = append(cur.lines, l)
	}
	if len(cur.lines) > 0 {
		sections = append(sections, cur)
	}

	var chunks []Chunk
	pending := Chunk{}
	flush := func() {
		if strings.TrimSpace(pending.Text) != "" {
			chunks = append(chunks, Chunk{Heading: pending.Heading, Text: strings.TrimSpace(pending.Text)})
		}
		pending = Chunk{}
	}
	for _, s := range sections {
		body := strings.TrimSpace(strings.Join(s.lines, "\n"))
		if body == "" {
			continue
		}
		if pending.Text == "" {
			pending.Heading = s.heading
		}
		for _, piece := range splitLong(body, ChunkMaxChars) {
			if pending.Text != "" && len(pending.Text)+len(piece)+2 > ChunkMaxChars {
				flush()
				pending.Heading = s.heading
			}
			if pending.Text != "" {
				pending.Text += "\n\n"
			}
			pending.Text += piece
			if len(pending.Text) >= chunkMinChars {
				flush()
				pending.Heading = s.heading
			}
		}
	}
	// a short last section has no next one to join: it goes with the chunk before, if that has room
	if t := strings.TrimSpace(pending.Text); t != "" && len(t) < chunkMinChars && len(chunks) > 0 &&
		len(chunks[len(chunks)-1].Text)+len(t)+2 <= ChunkMaxChars {
		chunks[len(chunks)-1].Text += "\n\n" + t
		pending = Chunk{}
	}
	flush()
	return chunks
}

// splitLong cuts text into pieces of at most max characters, at paragraph breaks, else at line breaks, else hard.
func splitLong(text string, max int) []string {
	if len(text) <= max {
		return []string{text}
	}
	var out []string
	for _, sep := range []string{"\n\n", "\n"} {
		parts := strings.Split(text, sep)
		if len(parts) == 1 {
			continue
		}
		cur := ""
		for _, p := range parts {
			if cur != "" && len(cur)+len(sep)+len(p) > max {
				out = append(out, splitLong(cur, max)...)
				cur = ""
			}
			if cur != "" {
				cur += sep
			}
			cur += p
		}
		if cur != "" {
			out = append(out, splitLong(cur, max)...)
		}
		return out
	}
	for len(text) > max {
		cut := max
		for cut > 0 && (text[cut]&0xC0) == 0x80 { // don't split a UTF-8 sequence
			cut--
		}
		out = append(out, text[:cut])
		text = text[cut:]
	}
	return append(out, text)
}

// chunkEmbedText is what gets embedded for a chunk: the note's title and the heading give it its context.
func chunkEmbedText(title string, c Chunk) string {
	if c.Heading != "" && !strings.HasPrefix(c.Text, "#") {
		return title + "\n" + c.Heading + "\n" + c.Text
	}
	return title + "\n" + c.Text
}

func titleOf(indexText string) string {
	title, _, _ := strings.Cut(indexText, "\n")
	return title
}
