package driver

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// sendRawPath writes the request line byte for byte.
func sendRawPath(t *testing.T, forwarder *CredentialForwarder, path string) int {
	t.Helper()
	connection, err := net.Dial("tcp4", forwarder.listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: %s\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}",
		path, forwarder.listener.Addr(), forwarder.Headers()["Authorization"])
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	require.NoError(t, err)
	_ = response.Body.Close()
	return response.StatusCode
}

func TestCredentialForwarderRefusesPathsThatLeaveTheServer(t *testing.T) {
	upstream, requests := newRecordingUpstream(t)
	forwarder, err := NewCredentialForwarder(map[string]UpstreamMCPServer{
		"tools": {URL: upstream.URL + "/mcp/tools"},
	}, 1<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })
	forwarder.Bind("Bearer person-token")

	for _, path := range []string{
		"/mcp/tools/../../other",
		"/mcp/tools/./x",
		"/mcp/tools/..",
		"/mcp/tools/%2e%2e/%2e%2e/other",
		"/mcp/tools/%2E%2e/other",
		"/mcp/tools/.%2e/other",
		"/mcp/tools/..%2f..%2fother",
		"/mcp/tools/a%2Fb",
		"/mcp/%2e%2e/tools",
		"/mcp/tools%2f..%2fother",
		"/mcp/tools/%252e%252e/%252e%252e/other",
		"/mcp/tools/..%5c..%5cother",
		"/mcp/tools/..\\..\\other",
		"/route/github.com/../../other",
		"/route/github.com/%2e%2e/%2e%2e/other",
		"/route/github.com/..%2f..%2fother",
		"/route/%2e%2e/mcp/tools",
	} {
		require.Equal(t, http.StatusBadRequest, sendRawPath(t, forwarder, path), path)
	}
	require.Empty(t, requests(), "a refused path reached the upstream")

	require.Equal(t, http.StatusOK, sendRawPath(t, forwarder, "/mcp/tools/a..b/.well-known"))
	seen := requests()
	require.Len(t, seen, 1)
	require.Equal(t, "/mcp/tools/a..b/.well-known", seen[0].Path)
}
