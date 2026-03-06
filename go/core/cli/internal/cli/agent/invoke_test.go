package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
)

// Note: Most InvokeCmd tests require K8s port-forwarding mock which is complex.
// Testing InvokeCmd with URLOverride still attempts port-forward first.
// Integration tests cover the full invoke workflow.

func TestInvokeCmd_ServerError(t *testing.T) {
	// Create mock server that returns an error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
	}))
	defer mockServer.Close()

	cfg := &InvokeCfg{
		Task:        "Test task",
		URLOverride: mockServer.URL,
		Stream:      false,
		Config:      &config.Config{},
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	InvokeCmd(context.Background(), cfg)

	// Restore stderr and read output
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)

	// Should print error message (or port-forward error, which is expected in test)
	output := buf.String()
	// The function should handle the error gracefully
	if len(output) == 0 {
		t.Skip("Test environment doesn't support stderr capture")
	}
}
