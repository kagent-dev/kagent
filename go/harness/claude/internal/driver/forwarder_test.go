package driver

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
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
			Authorization: request.Header.Get("Authorization"), Toolset: request.Header.Get("X-Muster-Toolset"),
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

func callForwarder(t *testing.T, forwarder *CredentialForwarder, url string, authorized bool) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	if authorized {
		for header, value := range forwarder.Headers() {
			request.Header.Set(header, value)
		}
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestCredentialForwarderCarriesTheTurnCredentialOnly(t *testing.T) {
	upstream, requests := newRecordingUpstream(t)
	forwarder, err := NewCredentialForwarder(map[string]UpstreamMCPServer{
		"muster": {URL: upstream.URL + "/mcp?tenant=lab", Headers: map[string]string{
			"X-Muster-Toolset": "preset:read-only", "Authorization": "Bearer static-service",
		}},
	}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = forwarder.Close() })
	if names := forwarder.Names(); len(names) != 1 || names[0] != "muster" {
		t.Fatalf("Names() = %v", names)
	}
	if !strings.HasPrefix(forwarder.URL("muster"), "http://127.0.0.1:") {
		t.Fatalf("URL() = %q, want a loopback address", forwarder.URL("muster"))
	}

	if response := callForwarder(t, forwarder, forwarder.URL("muster"), false); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated loopback call status = %d, want 401", response.StatusCode)
	}
	if seen := requests(); len(seen) != 0 {
		t.Fatalf("an unauthenticated loopback call reached the upstream: %#v", seen)
	}

	forwarder.Bind("Bearer person-token")
	if response := callForwarder(t, forwarder, forwarder.URL("muster"), true); response.StatusCode != http.StatusOK {
		t.Fatalf("forwarded call status = %d", response.StatusCode)
	}
	forwarder.Clear()
	if response := callForwarder(t, forwarder, forwarder.URL("muster")+"/session?id=7", true); response.StatusCode != http.StatusOK {
		t.Fatalf("forwarded call status = %d", response.StatusCode)
	}

	seen := requests()
	if len(seen) != 2 {
		t.Fatalf("upstream requests = %#v, want 2", seen)
	}
	want := []recordedRequest{
		{Path: "/mcp", Query: "tenant=lab", Authorization: "Bearer person-token", Toolset: "preset:read-only", Host: strings.TrimPrefix(upstream.URL, "http://")},
		{Path: "/mcp/session", Query: "tenant=lab&id=7", Authorization: "Bearer static-service", Toolset: "preset:read-only", Host: strings.TrimPrefix(upstream.URL, "http://")},
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("upstream request %d = %#v, want %#v", i, seen[i], want[i])
		}
		if strings.Contains(seen[i].Authorization, forwarder.token) {
			t.Errorf("the loopback token left the process: %#v", seen[i])
		}
	}
}

func TestCredentialForwarderRejectsUnknownServersAndInputs(t *testing.T) {
	upstream, requests := newRecordingUpstream(t)
	forwarder, err := NewCredentialForwarder(map[string]UpstreamMCPServer{"tools": {URL: upstream.URL}}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = forwarder.Close() })
	if response := callForwarder(t, forwarder, forwarder.URL("other"), true); response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown server status = %d, want 404", response.StatusCode)
	}
	if seen := requests(); len(seen) != 0 {
		t.Fatalf("an unknown server name reached the upstream: %#v", seen)
	}
	for name, servers := range map[string]map[string]UpstreamMCPServer{
		"none":          {},
		"relative URL":  {"tools": {URL: "/mcp"}},
		"bad scheme":    {"tools": {URL: "ftp://mcp.example.com"}},
		"name with '/'": {"a/b": {URL: upstream.URL}},
	} {
		if _, err := NewCredentialForwarder(servers, 1<<20); err == nil {
			t.Errorf("NewCredentialForwarder(%s) accepted invalid input", name)
		}
	}
}

func TestCredentialForwarderStreamsResponses(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := response.(http.Flusher)
		if !ok {
			t.Error("upstream response is not flushable")
			return
		}
		_, _ = io.WriteString(response, "event: message\ndata: first\n\n")
		flusher.Flush()
		<-release
		_, _ = io.WriteString(response, "event: message\ndata: second\n\n")
	}))
	t.Cleanup(upstream.Close)
	forwarder, err := NewCredentialForwarder(map[string]UpstreamMCPServer{"tools": {URL: upstream.URL}}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = forwarder.Close() })

	response := callForwarder(t, forwarder, forwarder.URL("tools"), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	first := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		first <- line
	}()
	select {
	case line := <-first:
		if line != "event: message\n" {
			t.Fatalf("first line = %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the forwarder buffered the stream until the upstream finished")
	}
	close(release)
	rest, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(rest), "data: second") {
		t.Fatalf("rest of stream = %q, %v", rest, err)
	}
}

func TestCallerCredentialReadsTheA2ACall(t *testing.T) {
	if got := CallerCredential(context.Background()); got != "" {
		t.Fatalf("CallerCredential(no call) = %q", got)
	}
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"x-user-id": {"someone"}}))
	if got := CallerCredential(ctx); got != "" {
		t.Fatalf("CallerCredential(no authorization) = %q", got)
	}
	ctx, _ = a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"authorization": {" Bearer person "}}))
	if got := CallerCredential(ctx); got != "Bearer person" {
		t.Fatalf("CallerCredential() = %q", got)
	}
}
