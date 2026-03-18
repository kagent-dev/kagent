package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/httpapi"
)

const (
	envKAgentURL = "KAGENT_URL"
)

// SandboxProvisioner calls the controller's sandbox endpoint to provision
// a sandbox for a session. It mirrors the Python ADK's sandbox provisioning
// in _ensure_sandbox_toolset.
type SandboxProvisioner struct {
	httpClient *http.Client
	kagentURL  string
}

// NewSandboxProvisioner creates a provisioner. If kagentURL is empty it falls
// back to the KAGENT_URL environment variable.
func NewSandboxProvisioner(httpClient *http.Client, kagentURL string) *SandboxProvisioner {
	if kagentURL == "" {
		kagentURL = os.Getenv(envKAgentURL)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &SandboxProvisioner{
		httpClient: httpClient,
		kagentURL:  kagentURL,
	}
}

// Provision calls POST /api/sessions/{sessionID}/sandbox on the controller,
// blocking until the sandbox is ready, and returns the MCP URL.
// The controller resolves the workspace from the session's agent CRD.
func (p *SandboxProvisioner) Provision(ctx context.Context, sessionID string) (string, error) {
	log := logr.FromContextOrDiscard(ctx)

	if p.kagentURL == "" {
		return "", fmt.Errorf("kagent URL is not configured; cannot provision sandbox")
	}

	url := fmt.Sprintf("%s/api/sessions/%s/sandbox", p.kagentURL, sessionID)
	log.Info("Provisioning sandbox", "url", url, "sessionID", sessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create sandbox request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sandbox provisioning request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read sandbox response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("sandbox provisioning failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var sandboxResp httpapi.SandboxResponse
	if err := json.Unmarshal(respBody, &sandboxResp); err != nil {
		return "", fmt.Errorf("failed to decode sandbox response: %w", err)
	}

	if sandboxResp.MCPUrl == "" {
		return "", fmt.Errorf("sandbox response missing mcp_url")
	}

	log.Info("Sandbox provisioned", "sessionID", sessionID, "mcpUrl", sandboxResp.MCPUrl, "ready", sandboxResp.Ready)
	return sandboxResp.MCPUrl, nil
}
