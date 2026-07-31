package models

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// TestComposeVertexHTTPClient_PreservesTimeout asserts the composed client
// inherits the caller's timeout so TransportConfig.Timeout is honored.
func TestComposeVertexHTTPClient_PreservesTimeout(t *testing.T) {
	base := &http.Client{Timeout: 42 * time.Second}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "unused"})

	got := composeVertexHTTPClient(base, ts)
	if got.Timeout != 42*time.Second {
		t.Errorf("Timeout = %v, want 42s", got.Timeout)
	}
	if _, ok := got.Transport.(*oauth2.Transport); !ok {
		t.Errorf("Transport = %T, want *oauth2.Transport", got.Transport)
	}
}

// TestComposeVertexHTTPClient_NilBaseTransport asserts the composed client
// falls back to http.DefaultTransport when base.Transport is nil, matching
// the standard library's behavior.
func TestComposeVertexHTTPClient_NilBaseTransport(t *testing.T) {
	base := &http.Client{}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "unused"})

	got := composeVertexHTTPClient(base, ts)
	oauthTransport, ok := got.Transport.(*oauth2.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *oauth2.Transport", got.Transport)
	}
	if oauthTransport.Base == nil {
		t.Errorf("oauth2.Transport.Base is nil, want http.DefaultTransport fallback")
	}
}

// TestComposeVertexHTTPClient_AttachesBearerToken verifies that requests made
// through the composed client carry an Authorization: Bearer <token> header
// sourced from the supplied oauth2.TokenSource. This is the regression guard
// for the original 401 bug.
func TestComposeVertexHTTPClient_AttachesBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	base := &http.Client{Transport: http.DefaultTransport, Timeout: 5 * time.Second}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"})
	client := composeVertexHTTPClient(base, ts)

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if want := "Bearer test-token"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

// TestComposeVertexHTTPClient_PreservesCustomHeaders verifies that
// TransportConfig.Headers set via the headerTransport survive composition,
// so operators' custom headers (e.g. proxy tokens) still reach the wire
// alongside the OAuth2 Authorization header.
func TestComposeVertexHTTPClient_PreservesCustomHeaders(t *testing.T) {
	var gotAuth, gotCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	base, err := BuildHTTPClient(TransportConfig{
		Headers: map[string]string{"X-Custom-Header": "custom-value"},
	})
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"})
	client := composeVertexHTTPClient(base, ts)

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if want := "Bearer test-token"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if want := "custom-value"; gotCustom != want {
		t.Errorf("X-Custom-Header = %q, want %q", gotCustom, want)
	}
}

// TestComposeVertexHTTPClient_PreservesBaseTransportType is a lightweight
// guard that the composed client's oauth2.Transport wraps the caller's
// transport chain (not a bare http.DefaultTransport). This gives us
// confidence that custom TLS config on the base transport survives
// composition, without needing a full mTLS harness.
func TestComposeVertexHTTPClient_PreservesBaseTransportType(t *testing.T) {
	insecure := true
	base, err := BuildHTTPClient(TransportConfig{TLSInsecureSkipVerify: &insecure})
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	if base.Transport == nil {
		t.Fatal("expected BuildHTTPClient to produce a non-nil Transport when TLS is customized")
	}
	originalTransport := base.Transport

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "unused"})
	client := composeVertexHTTPClient(base, ts)

	oauthTransport, ok := client.Transport.(*oauth2.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *oauth2.Transport", client.Transport)
	}
	if oauthTransport.Base != originalTransport {
		t.Errorf("oauth2.Transport.Base = %p, want caller's transport %p", oauthTransport.Base, originalTransport)
	}
}
