package modeldiscovery

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIDiscovery(t *testing.T) {
	for _, credential := range []string{"", "discovery-only"} {
		t.Run(credential, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/team/v1/models", r.URL.Path)
				want := ""
				if credential != "" {
					want = "Bearer " + credential
				}
				require.Equal(t, want, r.Header.Get("Authorization"))
				_, err := w.Write([]byte(`{"object":"list","data":[{"id":"zai-org/glm-5.2-maas","object":"model","created":1781308800,"owned_by":"gcp.vertex_ai","name":"GLM-5.2","context_window":1000000,"context_length":1000000,"max_input_tokens":1000000,"max_tokens":64000,"capabilities":{"tools":true,"temperature":false},"architecture":{"input_modalities":["text"]},"supported_parameters":["tools","max_tokens"],"future_metadata":{"x":1}}]}`))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newHTTPClient()
			defer client.CloseIdleConnections()
			models, err := fetch(t.Context(), client, server.URL+"/team/v1/", credential)
			require.NoError(t, err)
			require.Len(t, models, 1)
			model := models[0]
			require.Equal(t, "zai-org/glm-5.2-maas", model.Id)
			require.Equal(t, uint64(1000000), *model.ContextWindow)
			require.Equal(t, uint64(64000), *model.MaxTokens)
			require.True(t, *model.Capabilities.Tools)
			require.False(t, *model.Capabilities.Temperature)
			require.Nil(t, model.Capabilities.Reasoning)
			require.Equal(t, []string{"max_tokens", "tools"}, model.SupportedParameters)
		})
	}
}
func TestInvalidModelLists(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"object":"list","data":null}`, `{"object":"list","data":[null]}`, `{"object":"list","data":[{"id":"","object":"model"}]}`,
		`{"object":"list","data":[{"id":"a","object":"model"},{"id":"a","object":"model"}]}`,
		`{"object":"list","data":[],"has_more":true}`, `{"object":"list","data":[{"id":"a","object":"model","capabilities":{"tools":"yes"}}]}`,
		`{"object":"list","data":[{"id":"a","object":"model","max_tokens":-1}]}`,
	} {
		t.Run(body, func(t *testing.T) { _, err := parseModels([]byte(body)); require.Error(t, err) })
	}
	models, err := parseModels([]byte(`{"object":"list","data":[]}`))
	require.NoError(t, err)
	require.Empty(t, models)
	models, err = parseModels([]byte(`{"object":"list","data":[{"id":"basic","object":"model"}]}`))
	require.NoError(t, err)
	require.Nil(t, models[0].Capabilities)
}
func TestEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"", "ftp://host", "https:///v1", "https://user:pass@host/v1", "https://host/v1?limit=1", "https://host/v1#token"} {
		_, err := endpointURL(endpoint)
		require.Error(t, err, endpoint)
	}
	url, err := endpointURL("https://host/team%2Fa/v1/")
	require.NoError(t, err)
	require.Equal(t, "https://host/team%2Fa/v1/models", url)
}
func TestDiscoveryRedirectAndBodyLimits(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	client := newHTTPClient()
	defer client.CloseIdleConnections()
	_, err := fetch(t.Context(), client, server.URL, "secret")
	require.Error(t, err)
	require.Zero(t, calls)
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, strings.Repeat(" ", maxBody+1)) }))
	defer huge.Close()
	_, err = fetch(t.Context(), client, huge.URL, "")
	require.ErrorIs(t, err, errLimit)
}

func BenchmarkAgwCatalog(b *testing.B) {
	var body strings.Builder
	body.WriteString(`{"object":"list","data":[`)
	for i := range 1000 {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"id":"model-%d","object":"model","name":"Example","context_window":1000000,"max_tokens":64000,"capabilities":{"tools":true,"reasoning":true},"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`, i)
	}
	body.WriteString(`]}`)
	payload := []byte(body.String())
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		_, err := parseModels(payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}
