package forwarder

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordedRequest struct {
	Path, Query, Authorization, Toolset, Host string
}

func newRecordingUpstream(t *testing.T) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		seen = append(seen, recordedRequest{
			Path: request.URL.Path, Query: request.URL.RawQuery, Host: request.Host,
			Authorization: request.Header.Get("Authorization"), Toolset: request.Header.Get("X-Toolset"),
		})
		mu.Unlock()
		_, _ = io.WriteString(response, "ok")
	}))
	t.Cleanup(server.Close)
	return server, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), seen...)
	}
}

func newForwarder(t *testing.T, servers map[string]MCPServer) *Forwarder {
	t.Helper()
	forwarder, err := New(servers, 1<<20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = forwarder.Close() })
	return forwarder
}

func callForwarder(t *testing.T, forwarder *Forwarder, url string, authorized bool) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0"}`))
	require.NoError(t, err)
	if authorized {
		request.Header.Set("Authorization", forwarder.Authorization())
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestForwarderCarriesTheTurnCredentialOnly(t *testing.T) {
	upstream, requests := newRecordingUpstream(t)
	forwarder := newForwarder(t, map[string]MCPServer{
		"tools": {URL: upstream.URL + "/mcp?tenant=test", Headers: map[string]string{"x-toolset": "preset:read-only"}},
		"service": {URL: upstream.URL + "/service", Headers: map[string]string{
			"X-Toolset": "preset:read-only", "Authorization": "Bearer static-service",
		}},
	})
	require.True(t, strings.HasPrefix(forwarder.MCPURL("tools"), "http://127.0.0.1:"), forwarder.MCPURL("tools"))

	require.Equal(t, http.StatusUnauthorized, callForwarder(t, forwarder, forwarder.MCPURL("tools"), false).StatusCode)
	require.Empty(t, requests(), "an unauthenticated loopback call reached the upstream")

	release := forwarder.Bind("Bearer person-token")
	for _, name := range []string{"tools", "service"} {
		require.Equal(t, http.StatusOK, callForwarder(t, forwarder, forwarder.MCPURL(name), true).StatusCode)
	}
	release()
	require.Equal(t, http.StatusOK, callForwarder(t, forwarder, forwarder.MCPURL("tools")+"/session?id=7", true).StatusCode)

	host := strings.TrimPrefix(upstream.URL, "http://")
	require.Equal(t, []recordedRequest{
		{Path: "/mcp", Query: "tenant=test", Authorization: "Bearer person-token", Toolset: "preset:read-only", Host: host},
		// A static Authorization wins over the turn's credential, as in the Go ADK.
		{Path: "/service", Authorization: "Bearer static-service", Toolset: "preset:read-only", Host: host},
		// A request after the turn released its credential carries none.
		{Path: "/mcp/session", Query: "tenant=test&id=7", Toolset: "preset:read-only", Host: host},
	}, requests(), "the loopback token must never leave the process")
}

func TestForwarderPassthroughRequiresTheCallerCredential(t *testing.T) {
	upstream, requests := newRecordingUpstream(t)
	forwarder := newForwarder(t, map[string]MCPServer{
		"user": {URL: upstream.URL + "/mcp", TokenPassthrough: true, Headers: map[string]string{
			"X-Toolset": "preset:read-only", "Authorization": "Bearer static-service",
		}},
	})

	require.Equal(t, http.StatusUnauthorized, callForwarder(t, forwarder, forwarder.MCPURL("user"), true).StatusCode)
	require.Empty(t, requests(), "a passthrough call without a caller credential reached the upstream")

	forwarder.Bind("Bearer person-token")
	require.Equal(t, http.StatusOK, callForwarder(t, forwarder, forwarder.MCPURL("user"), true).StatusCode)
	require.Equal(t, []recordedRequest{
		{Path: "/mcp", Authorization: "Bearer person-token", Toolset: "preset:read-only", Host: strings.TrimPrefix(upstream.URL, "http://")},
	}, requests(), "a passthrough server carries the caller's credential only")
}

func TestForwarderRejectsUnknownServersAndInputs(t *testing.T) {
	upstream, requests := newRecordingUpstream(t)
	forwarder := newForwarder(t, map[string]MCPServer{"tools": {URL: upstream.URL}})
	require.Equal(t, http.StatusNotFound, callForwarder(t, forwarder, forwarder.MCPURL("other"), true).StatusCode)
	require.Empty(t, requests(), "an unknown server name reached the upstream")
	for name, servers := range map[string]map[string]MCPServer{
		"none":          {},
		"relative URL":  {"tools": {URL: "/mcp"}},
		"bad scheme":    {"tools": {URL: "ftp://mcp.example.com"}},
		"name with '/'": {"a/b": {URL: upstream.URL}},
	} {
		_, err := New(servers, 1<<20)
		require.Error(t, err, name)
	}
}

func TestForwarderStreamsResponses(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(response, "event: message\ndata: first\n\n")
		response.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(response, "event: message\ndata: second\n\n")
	}))
	t.Cleanup(upstream.Close)
	forwarder := newForwarder(t, map[string]MCPServer{"tools": {URL: upstream.URL}})

	response := callForwarder(t, forwarder, forwarder.MCPURL("tools"), true)
	require.Equal(t, http.StatusOK, response.StatusCode)
	reader := bufio.NewReader(response.Body)
	first := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		first <- line
	}()
	select {
	case line := <-first:
		require.Equal(t, "event: message\n", line)
	case <-time.After(5 * time.Second):
		t.Fatal("the forwarder buffered the stream until the upstream finished")
	}
	close(release)
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(rest), "data: second")
}

func TestForwarderKeepsThePathAbsoluteForAPathlessUpstream(t *testing.T) {
	upstream, requests := newRecordingUpstream(t)
	forwarder := newForwarder(t, map[string]MCPServer{"tools": {URL: upstream.URL}})
	forwarder.Bind("Bearer person-token")
	require.Equal(t, http.StatusOK, callForwarder(t, forwarder, forwarder.MCPURL("tools")+"/session", true).StatusCode)
	seen := requests()
	require.Len(t, seen, 1)
	require.Equal(t, "/session", seen[0].Path)
}

func TestResolveHeaders(t *testing.T) {
	environment := []string{"KAGENT_SECRET=from-secret", "EMPTY="}
	for _, test := range []struct {
		name    string
		headers map[string]string
		want    map[string]string
		wantErr bool
	}{
		{name: "none"},
		{name: "literal", headers: map[string]string{"X-Toolset": "read-only"}, want: map[string]string{"X-Toolset": "read-only"}},
		{name: "reference", headers: map[string]string{"Authorization": "${KAGENT_SECRET}"}, want: map[string]string{"Authorization": "from-secret"}},
		{name: "embedded reference stays literal", headers: map[string]string{"X-Note": "Bearer ${KAGENT_SECRET}"}, want: map[string]string{"X-Note": "Bearer ${KAGENT_SECRET}"}},
		{name: "unset", headers: map[string]string{"Authorization": "${MISSING}"}, wantErr: true},
		{name: "empty", headers: map[string]string{"Authorization": "${EMPTY}"}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveHeaders(test.headers, environment)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
