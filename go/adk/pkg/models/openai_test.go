package models

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAIModel_APIKey(t *testing.T) {
	for _, tt := range []struct {
		name        string
		env         string
		apiKey      string
		customURL   bool
		passthrough bool
		want        string
		wantErr     bool
	}{
		{name: "no key and no base URL fails", wantErr: true},
		{name: "no key with custom base URL is unauthenticated", customURL: true},
		{name: "env key with custom base URL", env: "sk-env", customURL: true, want: "Bearer sk-env"},
		{name: "env key without base URL", env: "sk-env", want: "Bearer sk-env"},
		{name: "explicit key without env", apiKey: "sk-explicit", want: "Bearer sk-explicit"},
		{name: "explicit key overrides env", env: "sk-env", apiKey: "sk-explicit", want: "Bearer sk-explicit"},
		{name: "passthrough without key", passthrough: true, want: "Bearer passthrough"},
		{name: "passthrough ignores configured keys", env: "sk-env", apiKey: "sk-explicit", passthrough: true, want: "Bearer passthrough"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Values("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			}))
			t.Cleanup(srv.Close)

			t.Setenv("OPENAI_API_KEY", tt.env)
			cfg := &OpenAIConfig{
				TransportConfig: TransportConfig{APIKeyPassthrough: tt.passthrough},
				Model:           "llama",
				APIKey:          tt.apiKey,
			}
			if tt.customURL {
				cfg.BaseUrl = srv.URL
			}
			m, err := NewOpenAIModel(t.Context(), cfg)
			if tt.wantErr {
				require.EqualError(t, err, "OPENAI_API_KEY environment variable is not set")
				return
			}
			require.NoError(t, err)
			// Keep requests local even when testing the default endpoint's key requirement.
			_, err = m.Client.Models.List(t.Context(), option.WithBaseURL(srv.URL))
			require.NoError(t, err)
			if tt.want == "" {
				require.Empty(t, gotAuth)
			} else {
				require.Equal(t, []string{tt.want}, gotAuth)
			}
		})
	}
}
