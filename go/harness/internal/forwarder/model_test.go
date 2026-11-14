package forwarder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type modelRequest struct {
	path   string
	header http.Header
}

func newModelUpstream(t *testing.T) (*httptest.Server, func() []modelRequest) {
	t.Helper()
	requests := make(chan modelRequest, 16)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requests <- modelRequest{path: request.URL.Path, header: request.Header.Clone()}
	}))
	t.Cleanup(server.Close)
	return server, func() []modelRequest {
		var seen []modelRequest
		for {
			select {
			case request := <-requests:
				seen = append(seen, request)
			default:
				return seen
			}
		}
	}
}

func TestForwarderSendsTheCallerTokenAsTheModelAPIKey(t *testing.T) {
	for _, test := range []struct {
		name           string
		model          Model
		authenticate   func(*http.Request, *Forwarder)
		header, prefix string
	}{
		{
			name:  "Anthropic reads X-Api-Key",
			model: Model{Header: "x-api-key"}, header: "X-Api-Key",
			authenticate: func(request *http.Request, forwarder *Forwarder) {
				request.Header.Set("X-Api-Key", forwarder.Token())
			},
		},
		{
			name:  "OpenAI reads a bearer Authorization",
			model: Model{Header: "Authorization", Prefix: "Bearer "}, header: "Authorization", prefix: "Bearer ",
			authenticate: func(request *http.Request, forwarder *Forwarder) {
				request.Header.Set("Authorization", forwarder.Authorization())
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream, requests := newModelUpstream(t)
			test.model.URL = upstream.URL + "/v1"
			forwarder, err := New(Routes{Model: &test.model}, 1)
			require.NoError(t, err)
			t.Cleanup(func() { _ = forwarder.Close() })
			call := func() int {
				request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, forwarder.ModelURL()+"/messages", strings.NewReader(`{"model":"m","messages":[]}`))
				require.NoError(t, err)
				test.authenticate(request, forwarder)
				request.Header.Set("Anthropic-Version", "2023-06-01")
				request.Header.Set("Cookie", "session=other")
				response, err := http.DefaultClient.Do(request)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				return response.StatusCode
			}

			require.Equal(t, http.StatusUnauthorized, call(), "a turn without a caller token must not reach the model")
			release := forwarder.Bind("Basic not-a-bearer")
			require.Equal(t, http.StatusUnauthorized, call(), "only a bearer token is an API key")
			release()
			require.Empty(t, requests())

			defer forwarder.Bind("Bearer person-token")()
			// The MCP body limit does not apply to model requests.
			require.Equal(t, http.StatusOK, call())
			seen := requests()
			require.Len(t, seen, 1)
			require.Equal(t, "/v1/messages", seen[0].path)
			require.Equal(t, test.prefix+"person-token", seen[0].header.Get(test.header))
			require.Equal(t, "2023-06-01", seen[0].header.Get("Anthropic-Version"), "provider headers reach the model")
			require.Empty(t, seen[0].header.Get("Cookie"))
			for _, header := range credentialHeaders {
				if header != test.header {
					require.Empty(t, seen[0].header.Get(header), header)
				}
			}
			require.NotContains(t, seen[0].header.Get(test.header), forwarder.Token(), "the loopback token left the process")
		})
	}
}

func TestForwarderRoutesOnlyConfiguredUpstreams(t *testing.T) {
	upstream, requests := newModelUpstream(t)
	mcpOnly := newForwarder(t, map[string]MCPServer{"tools": {URL: upstream.URL}})
	defer mcpOnly.Bind("Bearer person-token")()
	require.Equal(t, http.StatusNotFound, callForwarder(t, mcpOnly, mcpOnly.ModelURL()+"/messages", true).StatusCode)

	modelOnly, err := New(Routes{Model: &Model{URL: upstream.URL, Header: "X-Api-Key"}}, 1<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = modelOnly.Close() })
	defer modelOnly.Bind("Bearer person-token")()
	require.Equal(t, http.StatusNotFound, callForwarder(t, modelOnly, modelOnly.MCPURL("tools"), true).StatusCode)
	require.Equal(t, http.StatusUnauthorized, callForwarder(t, modelOnly, modelOnly.ModelURL(), false).StatusCode)
	require.Equal(t, http.StatusBadRequest, sendRawPath(t, modelOnly, "/model/../mcp/tools"))
	require.Empty(t, requests())

	for name, routes := range map[string]Routes{
		"none":            {},
		"relative model":  {Model: &Model{URL: "/v1", Header: "X-Api-Key"}},
		"model no header": {Model: &Model{URL: upstream.URL}},
	} {
		_, err := New(routes, 1<<20)
		require.Error(t, err, name)
	}
}
