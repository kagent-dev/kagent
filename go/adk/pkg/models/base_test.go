package models

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"
)

func TestMergeSystemInstructionFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		config   *genai.GenerateContentConfig
		want     string
	}{
		{
			name:     "nil config returns trimmed existing",
			existing: "  hello  ",
			want:     "hello",
		},
		{
			name: "config only",
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{
						{Text: "You are helpful."},
						{Text: "Be concise."},
					},
				},
			},
			want: "You are helpful.\nBe concise.",
		},
		{
			name: "skips empty text parts",
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{
						{Text: "  one  "},
						{Text: ""},
						{Text: "two"},
					},
				},
			},
			want: "one  \ntwo",
		},
		{
			name:     "merges existing with config",
			existing: "From contents",
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{{Text: "From config"}},
				},
			},
			want: "From contents\nFrom config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeSystemInstructionFromConfig(tt.existing, tt.config)
			if got != tt.want {
				t.Errorf("mergeSystemInstructionFromConfig() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeaderTransport_SendsHostAsRequestHost(t *testing.T) {
	t.Parallel()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &headerTransport{
		base:    srv.Client().Transport,
		headers: map[string]string{"Host": "tenant.gateway.internal"},
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	resp.Body.Close()
	if seen != "tenant.gateway.internal" {
		t.Errorf("server Host = %q, want %q", seen, "tenant.gateway.internal")
	}
}
