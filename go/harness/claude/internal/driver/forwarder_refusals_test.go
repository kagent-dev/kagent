package driver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// The forwarder counts what it answers itself, by the status it answered, and
// never what an upstream answers: an upstream's 401 or 404 is the server's.
func TestCredentialForwarderCountsItsOwnRefusalsOnce(t *testing.T) {
	upstream := newStatusUpstream(t, http.StatusUnauthorized)
	forwarder, err := NewCredentialForwarder(map[string]UpstreamMCPServer{"tools": {URL: upstream + "/mcp"}}, 1<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })

	require.Equal(t, http.StatusUnauthorized, callForwarder(t, forwarder, forwarder.URL("tools"), false).StatusCode)
	require.Equal(t, http.StatusUnauthorized, callForwarder(t, forwarder, forwarder.URL("other"), false).StatusCode)
	require.Equal(t, http.StatusNotFound, callForwarder(t, forwarder, forwarder.URL("other"), true).StatusCode)
	require.Equal(t, http.StatusNotFound, sendRawPath(t, forwarder, "/other"))
	require.Equal(t, http.StatusNotFound, sendRawPath(t, forwarder, "/.well-known/oauth-protected-resource/mcp/tools"), "OAuth discovery is no refusal")
	require.Equal(t, http.StatusBadRequest, sendRawPath(t, forwarder, "/mcp/tools/../../other"))
	require.Equal(t, http.StatusUnauthorized, callForwarder(t, forwarder, forwarder.URL("tools"), true).StatusCode, "the upstream's own 401")

	require.Equal(t, apia2a.ForwarderRefusals{Unauthorized: 2, UnknownServer: 2, RefusedPath: 1}, forwarder.TakeRefusals())
	require.True(t, forwarder.TakeRefusals().IsZero(), "a refusal is reported once")
}

func newStatusUpstream(t *testing.T, status int) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server.URL
}
