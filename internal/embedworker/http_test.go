package embedworker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeServer embeds each text as [len(text)], answers in reverse order, and
// rejects texts longer than maxLen characters like llama.cpp does.
func fakeServer(t *testing.T, maxLen int, calls *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var req httpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		for _, s := range req.Input {
			if len(s) > maxLen {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"type": "exceed_context_size_error", "n_prompt_tokens": len(s), "n_ctx": maxLen}})
				return
			}
		}
		var resp httpResponse
		for i := len(req.Input) - 1; i >= 0; i-- {
			resp.Data = append(resp.Data, struct {
				Index     int       `json:"index"`
				Embedding []float64 `json:"embedding"`
			}{i, []float64{float64(len(req.Input[i]))}})
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestHTTPEmbedderOrderAndBatches(t *testing.T) {
	calls := 0
	srv := fakeServer(t, 1000, &calls)
	defer srv.Close()
	texts := make([]string, 70)
	for i := range texts {
		texts[i] = string(make([]byte, i+1))
	}
	vecs, err := NewHTTPEmbedder(srv.URL, "")(texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 70 || calls != 3 {
		t.Fatalf("got %d vectors in %d calls, want 70 in 3", len(vecs), calls)
	}
	for i, v := range vecs {
		if v[0] != float64(i+1) {
			t.Fatalf("vector %d = %v, out of order", i, v)
		}
	}
}

func TestHTTPEmbedderTruncatesTooLong(t *testing.T) {
	calls := 0
	srv := fakeServer(t, 100, &calls)
	defer srv.Close()
	vecs, err := NewHTTPEmbedder(srv.URL, "")([]string{"short", string(make([]byte, 400))})
	if err != nil {
		t.Fatal(err)
	}
	if vecs[0][0] != 5 || vecs[1][0] > 100 || vecs[1][0] < 50 {
		t.Fatalf("got %v, want short kept and long cut to <= 100", vecs)
	}
}

func TestResolveURLEnv(t *testing.T) {
	t.Setenv("WIKI_CONFIG", t.TempDir()+"/none.yaml")
	t.Setenv("WIKI_EMBED_URL", "http://example/v1/embeddings")
	if url, _, err := ResolveURL(); err != nil || url != "http://example/v1/embeddings" {
		t.Fatalf("ResolveURL = %q, %v", url, err)
	}
}

func TestUnreachableIsDetected(t *testing.T) {
	_, err := NewHTTPEmbedder("http://127.0.0.1:1/v1/embeddings", "")([]string{"x"})
	if err == nil || !isUnreachable(err) {
		t.Fatalf("err = %v, want unreachable", err)
	}
}

func TestHTTPEmbedderTruncatesBatchTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req httpRequest
		json.NewDecoder(r.Body).Decode(&req)
		for _, s := range req.Input {
			if len(s) > 2000 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":{"code":500,"message":"input (9531 tokens) is too large to process. increase the physical batch size (current batch size: 4096)","type":"server_error"}}`))
				return
			}
		}
		resp := map[string]any{"data": []map[string]any{}}
		for i, s := range req.Input {
			resp["data"] = append(resp["data"].([]map[string]any), map[string]any{"index": i, "embedding": []float64{float64(len(s))}})
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	vecs, err := NewHTTPEmbedder(srv.URL, "")([]string{string(make([]byte, 20000))})
	if err != nil || vecs[0][0] > 2000 {
		t.Fatalf("got %v, %v", vecs, err)
	}
}
