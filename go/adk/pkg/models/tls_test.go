package models

import (
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTLSServer starts a test HTTPS server that always returns 200.
func newTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serverCAPEMPath writes the test server's certificate to a temp file and returns the path.
func serverCAPEMPath(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	data := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: srv.Certificate().Raw,
	})
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write CA PEM: %v", err)
	}
	return path
}

// get is a helper that makes a GET request and returns the status code.
func get(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// --- BuildTLSTransport ---

func TestBuildTLSTransport_NilConfig_ReturnsBase(t *testing.T) {
	base := http.DefaultTransport
	transport, err := BuildTLSTransport(base, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport != base {
		t.Error("expected base to be returned unchanged when no TLS config is set")
	}
}

func TestBuildTLSTransport_CAFileNotFound(t *testing.T) {
	path := "/nonexistent/ca.pem"
	_, err := BuildTLSTransport(nil, nil, &path, nil)
	if err == nil {
		t.Error("expected error for missing CA file")
	}
}

// Should reject self-signed cert by default
func TestBuildHTTPClient_DefaultRejectsSelfsigned(t *testing.T) {
	srv := newTLSServer(t)
	client, err := BuildHTTPClient(TransportConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected TLS error for self-signed cert with no config")
	}
}

// Should accept self-signed cert if insecure skip verify is set
func TestBuildHTTPClient_InsecureSkipVerify(t *testing.T) {
	srv := newTLSServer(t)
	insecure := true
	client, err := BuildHTTPClient(TransportConfig{TLSInsecureSkipVerify: &insecure})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status := get(t, client, srv.URL); status != http.StatusOK {
		t.Errorf("expected 200, got %d", status)
	}
}

// Should accept custom CA if specified
func TestBuildHTTPClient_CustomCA(t *testing.T) {
	srv := newTLSServer(t)
	caPath := serverCAPEMPath(t, srv)
	client, err := BuildHTTPClient(TransportConfig{TLSCACertPath: &caPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status := get(t, client, srv.URL); status != http.StatusOK {
		t.Errorf("expected 200, got %d", status)
	}
}

// Should accept custom CA if specified and system CAs are disabled
func TestBuildHTTPClient_CustomCA_DisableSystemCAs(t *testing.T) {
	srv := newTLSServer(t)
	caPath := serverCAPEMPath(t, srv)
	disableSystem := true
	client, err := BuildHTTPClient(TransportConfig{
		TLSCACertPath:       &caPath,
		TLSDisableSystemCAs: &disableSystem,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status := get(t, client, srv.URL); status != http.StatusOK {
		t.Errorf("expected 200, got %d", status)
	}
}

// Should set timeout if specified
func TestBuildHTTPClient_Timeout(t *testing.T) {
	seconds := 42
	client, err := BuildHTTPClient(TransportConfig{Timeout: &seconds})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Timeout != 42*time.Second {
		t.Errorf("expected timeout 42s, got %v", client.Timeout)
	}
}

// Should set a connect timeout on a cloned transport without mutating the
// shared http.DefaultTransport.
func TestBuildHTTPClient_ConnectTimeout(t *testing.T) {
	def, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is %T, want *http.Transport", http.DefaultTransport)
	}
	origDial := fmt.Sprintf("%p", def.DialContext)

	seconds := 7
	client, err := BuildHTTPClient(TransportConfig{ConnectTimeout: &seconds})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if tr == def {
		t.Fatal("expected a cloned transport, got shared http.DefaultTransport")
	}
	if tr.DialContext == nil {
		t.Fatal("expected DialContext to be set for connect timeout")
	}
	if newDial := fmt.Sprintf("%p", def.DialContext); newDial != origDial {
		t.Error("http.DefaultTransport.DialContext was mutated by BuildHTTPClient")
	}
}

// Should inject headers if specified
func TestBuildHTTPClient_HeadersInjected(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Test")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client, err := BuildHTTPClient(TransportConfig{Headers: map[string]string{"X-Test": "hello"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	get(t, client, srv.URL)
	if got != "hello" {
		t.Errorf("expected X-Test 'hello', got %q", got)
	}
}
