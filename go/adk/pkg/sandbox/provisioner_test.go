package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/api/httpapi"
)

func TestProvisionSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/sessions/sess-1/sandbox" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Body should be empty — controller resolves workspace from session.
		if r.ContentLength > 0 {
			t.Errorf("expected empty body, got content-length %d", r.ContentLength)
		}

		resp := httpapi.SandboxResponse{
			SandboxID: "sb-123",
			MCPUrl:    "http://sandbox-pod:8080/mcp",
			Protocol:  "streamable-http",
			Ready:     true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewSandboxProvisioner(server.Client(), server.URL)

	mcpURL, err := p.Provision(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if mcpURL != "http://sandbox-pod:8080/mcp" {
		t.Errorf("unexpected mcpURL: %s", mcpURL)
	}
}

func TestProvisionServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	p := NewSandboxProvisioner(server.Client(), server.URL)

	_, err := p.Provision(context.Background(), "sess-1")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestProvisionNoKagentURL(t *testing.T) {
	p := NewSandboxProvisioner(nil, "")

	_, err := p.Provision(context.Background(), "sess-1")
	if err == nil {
		t.Fatal("expected error when kagent URL is empty")
	}
}
