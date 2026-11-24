package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/ollama/ollama/api"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// TestOllamaOptionsDecodeOnTheServer sends ModelConfig options through
// GenerateContent to a test server that reads them with api.Options.FromMap,
// like the Ollama server does. A known option with the wrong type fails the
// request, same as with a real Ollama.
func TestOllamaOptionsDecodeOnTheServer(t *testing.T) {
	scenarios := []struct {
		given   string
		options map[string]string
		want    api.Options
	}{
		{
			given:   "Given num_batch",
			options: map[string]string{"num_batch": "512"},
			want:    api.Options{Runner: api.Runner{NumBatch: 512}},
		},
		{
			given:   "Given main_gpu and use_mmap",
			options: map[string]string{"main_gpu": "0", "use_mmap": "false"},
			want:    api.Options{Runner: api.Runner{MainGPU: new(0), UseMMap: new(false)}},
		},
		{
			given:   "Given one stop sequence",
			options: map[string]string{"stop": "[INST]"},
			want:    api.Options{Stop: []string{"[INST]"}},
		},
		{
			given:   "Given stop sequences as a JSON array",
			options: map[string]string{"stop": `["<|im_end|>", "[INST]"]`},
			want:    api.Options{Stop: []string{"<|im_end|>", "[INST]"}},
		},
		{
			given:   "Given num_ctx and temperature",
			options: map[string]string{"num_ctx": "4096", "temperature": "0.25"},
			want:    api.Options{Runner: api.Runner{NumCtx: 4096}, Temperature: 0.25},
		},
	}

	for _, s := range scenarios {
		for _, stream := range []bool{false, true} {
			name := s.given + ", when the model is called, assert that Ollama decodes the options"
			if stream {
				name = s.given + ", when the model is streamed, assert that Ollama decodes the options"
			}
			t.Run(name, func(t *testing.T) {
				server := newOptionsDecodingServer(t)
				m := &OllamaModel{
					Config: &OllamaConfig{Model: "qwen2.5:0.5b", Options: s.options},
					Client: api.NewClient(server.url, http.DefaultClient),
				}
				req := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "say hi"}}}}}

				for resp, err := range m.GenerateContent(t.Context(), req, stream) {
					if err != nil {
						t.Fatalf("GenerateContent error: %v", err)
					}
					if resp.ErrorMessage != "" {
						t.Fatalf("Ollama rejected the request: %s", resp.ErrorMessage)
					}
				}

				// The handler decodes before it answers, so the value is
				// already in the channel here.
				select {
				case got := <-server.decoded:
					if !reflect.DeepEqual(got, s.want) {
						t.Errorf("decoded options = %+v, want %+v", got, s.want)
					}
				default:
					t.Fatal("the server never decoded the request options")
				}
			})
		}
	}
}

type optionsDecodingServer struct {
	url     *url.URL
	decoded chan api.Options
}

// newOptionsDecodingServer reads the request options like the Ollama server
// does. If they do not decode, it returns 500 with Ollama's error body.
func newOptionsDecodingServer(t *testing.T) *optionsDecodingServer {
	t.Helper()
	s := &optionsDecodingServer{decoded: make(chan api.Options, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var opts api.Options
		if err := opts.FromMap(req.Options); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		s.decoded <- opts
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"model":"qwen2.5:0.5b","message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	s.url = u
	return s
}
