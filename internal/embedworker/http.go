package embedworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"time"

	"wiki_skill/internal/config"
)

// httpBatch is how many texts go into one embeddings request.
const httpBatch = 32

type httpRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model,omitempty"`
}

type httpResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// llama.cpp's error for an input longer than the context, e.g.
// {"error":{"type":"exceed_context_size_error","n_prompt_tokens":1002,"n_ctx":512}}
type httpError struct {
	Error struct {
		Type          string `json:"type"`
		NPromptTokens int    `json:"n_prompt_tokens"`
		NCtx          int    `json:"n_ctx"`
	} `json:"error"`
}

// NewHTTPEmbedder embeds through an OpenAI-compatible /v1/embeddings endpoint
// (e.g. a llama-server with --embeddings, or a proxy in front of one that
// decides GPU or CPU). Texts longer than the server's context are cut and
// retried one by one, the way local embedders truncate to the model's limit.
func NewHTTPEmbedder(url, model string) Embedder {
	client := &http.Client{Timeout: 10 * time.Minute}
	return func(texts []string) ([][]float64, error) {
		out := make([][]float64, 0, len(texts))
		for start := 0; start < len(texts); start += httpBatch {
			end := min(start+httpBatch, len(texts))
			vecs, tooLong, err := postEmbeddings(client, url, model, texts[start:end])
			if tooLong {
				vecs = make([][]float64, 0, end-start)
				for _, t := range texts[start:end] {
					v, err := embedOneTruncating(client, url, model, t)
					if err != nil {
						return nil, err
					}
					vecs = append(vecs, v)
				}
			} else if err != nil {
				return nil, err
			}
			out = append(out, vecs...)
		}
		return out, nil
	}
}

func embedOneTruncating(client *http.Client, url, model, text string) ([]float64, error) {
	runes := []rune(text)
	for attempt := 0; attempt < 6; attempt++ {
		vecs, tooLong, err := postEmbeddings(client, url, model, []string{string(runes)})
		if !tooLong {
			if err != nil {
				return nil, err
			}
			return vecs[0], nil
		}
		ratio := 0.5
		if e, ok := err.(*contextError); ok && e.nPrompt > 0 && e.nCtx > 0 {
			ratio = float64(e.nCtx) / float64(e.nPrompt) * 0.95
		}
		runes = runes[:max(1, int(float64(len(runes))*ratio))]
	}
	return nil, fmt.Errorf("embeddings: text still too long after truncation")
}

type contextError struct{ nPrompt, nCtx int }

func (e *contextError) Error() string {
	return fmt.Sprintf("input of %d tokens exceeds the context of %d", e.nPrompt, e.nCtx)
}

// postEmbeddings returns the vectors in input order. tooLong reports a
// context-size error (err is then a *contextError).
func postEmbeddings(client *http.Client, url, model string, texts []string) ([][]float64, bool, error) {
	body, err := json.Marshal(httpRequest{Input: texts, Model: model})
	if err != nil {
		return nil, false, err
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, false, &unreachableError{err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode != http.StatusOK {
		var he httpError
		if json.Unmarshal(data, &he) == nil && he.Error.Type == "exceed_context_size_error" {
			return nil, true, &contextError{he.Error.NPromptTokens, he.Error.NCtx}
		}
		// Inputs far past the batch size get a 500 instead:
		// "input (9531 tokens) is too large to process. increase the physical batch size ..."
		if m := tooLargeRE.FindSubmatch(data); m != nil {
			n, _ := strconv.Atoi(string(m[1]))
			return nil, true, &contextError{n, 512}
		}
		if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable {
			return nil, false, &unreachableError{fmt.Errorf("HTTP %d", resp.StatusCode)}
		}
		return nil, false, fmt.Errorf("embeddings: HTTP %d: %.300s", resp.StatusCode, data)
	}
	var r httpResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, false, fmt.Errorf("embeddings returned invalid JSON: %w", err)
	}
	if len(r.Data) != len(texts) {
		return nil, false, fmt.Errorf("embeddings: got %d vectors for %d texts", len(r.Data), len(texts))
	}
	sort.Slice(r.Data, func(i, j int) bool { return r.Data[i].Index < r.Data[j].Index })
	vecs := make([][]float64, len(r.Data))
	for i, d := range r.Data {
		vecs[i] = d.Embedding
	}
	return vecs, false, nil
}

var tooLargeRE = regexp.MustCompile(`input \((\d+) tokens\) is too large to process`)

// isUnreachable reports a failed connection (as opposed to an HTTP error).
func isUnreachable(err error) bool {
	var ue *unreachableError
	return errors.As(err, &ue)
}

type unreachableError struct{ err error }

func (e *unreachableError) Error() string { return "embeddings request failed: " + e.err.Error() }
func (e *unreachableError) Unwrap() error { return e.err }

// ResolveURL returns the embeddings endpoint: $WIKI_EMBED_URL, then embed_url
// in the config file, else "" (use the local worker).
func ResolveURL() (url, model string, err error) {
	cfg, err := config.Load()
	if err != nil {
		return "", "", err
	}
	url = cfg.EmbedURL
	if env := os.Getenv("WIKI_EMBED_URL"); env != "" {
		url = env
	}
	return url, cfg.EmbedModel, nil
}
