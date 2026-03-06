package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kagentBaseURL() string {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		kagentURL = "http://localhost:8083"
	}
	return kagentURL
}

// TestE2EPluginRouting verifies the full plugin routing pipeline:
// 1. Create RemoteMCPServer with ui section
// 2. Wait for controller to reconcile (poll /api/plugins)
// 3. Verify /api/plugins returns correct metadata
// 4. Delete CRD
// 5. Verify /api/plugins no longer returns the entry
// 6. Verify /_p/{name}/ returns 404
func TestE2EPluginRouting(t *testing.T) {
	cli := setupK8sClient(t, false)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	baseURL := kagentBaseURL()

	// Create a RemoteMCPServer with UI metadata
	rmcps := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-plugin-ui-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "Test plugin for E2E routing",
			Protocol:    v1alpha2.RemoteMCPServerProtocolStreamableHttp,
			URL:         "http://test-plugin-svc.kagent.svc:8080/mcp",
			UI: &v1alpha2.PluginUISpec{
				Enabled:     true,
				PathPrefix:  "test-plugin",
				DisplayName: "Test Plugin",
				Icon:        "puzzle",
				Section:     "PLUGINS",
			},
		},
	}

	err := cli.Create(t.Context(), rmcps)
	require.NoError(t, err, "failed to create RemoteMCPServer with UI")
	t.Logf("Created RemoteMCPServer %s", rmcps.Name)
	cleanup(t, cli, rmcps)

	// Poll /api/plugins until the plugin appears
	pluginsURL := baseURL + "/api/plugins"
	t.Logf("Polling %s for plugin to appear", pluginsURL)

	var foundPlugin *handlers.PluginResponse
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pluginsURL, nil)
		if err != nil {
			return false, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Logf("Request to %s failed: %v", pluginsURL, err)
			return false, nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Logf("GET %s returned %d", pluginsURL, resp.StatusCode)
			return false, nil
		}

		var body api.StandardResponse[[]handlers.PluginResponse]
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Logf("Failed to decode response: %v", err)
			return false, nil
		}

		for i, p := range body.Data {
			if p.PathPrefix == "test-plugin" {
				foundPlugin = &body.Data[i]
				return true, nil
			}
		}

		t.Logf("Plugin not yet in /api/plugins (got %d plugins)", len(body.Data))
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for plugin to appear in /api/plugins")

	// Verify plugin metadata
	require.NotNil(t, foundPlugin)
	assert.Equal(t, "test-plugin", foundPlugin.PathPrefix)
	assert.Equal(t, "Test Plugin", foundPlugin.DisplayName)
	assert.Equal(t, "puzzle", foundPlugin.Icon)
	assert.Equal(t, "PLUGINS", foundPlugin.Section)
	t.Logf("Plugin metadata verified: %+v", foundPlugin)

	// Verify /_p/test-plugin/ returns a response (proxy is set up)
	// The upstream doesn't exist, so we expect a 502 (Bad Gateway) rather than 404
	proxyURL := baseURL + "/_p/test-plugin/"
	proxyReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proxyURL, nil)
	require.NoError(t, err)
	proxyResp, err := httpClient.Do(proxyReq)
	require.NoError(t, err)
	proxyResp.Body.Close()
	// Should NOT be 404 (that would mean plugin routing isn't set up)
	assert.NotEqual(t, http.StatusNotFound, proxyResp.StatusCode,
		"expected proxy to be configured (got 404, meaning plugin not found in DB)")
	t.Logf("Proxy endpoint %s returned %d (expected non-404)", proxyURL, proxyResp.StatusCode)

	// Delete the CRD
	t.Logf("Deleting RemoteMCPServer %s", rmcps.Name)
	err = cli.Delete(t.Context(), rmcps)
	require.NoError(t, err)

	// Poll until plugin disappears from /api/plugins
	t.Logf("Waiting for plugin to disappear from /api/plugins")
	disappearErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pluginsURL, nil)
		if err != nil {
			return false, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()

		var body api.StandardResponse[[]handlers.PluginResponse]
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false, nil
		}

		for _, p := range body.Data {
			if p.PathPrefix == "test-plugin" {
				t.Logf("Plugin still present in /api/plugins")
				return false, nil
			}
		}
		return true, nil
	})
	require.NoError(t, disappearErr, "timed out waiting for plugin to disappear from /api/plugins")
	t.Logf("Plugin removed from /api/plugins after CRD deletion")

	// Verify /_p/test-plugin/ returns 404 after deletion
	proxyReq2, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proxyURL, nil)
	require.NoError(t, err)
	proxyResp2, err := httpClient.Do(proxyReq2)
	require.NoError(t, err)
	proxyResp2.Body.Close()
	assert.Equal(t, http.StatusNotFound, proxyResp2.StatusCode,
		fmt.Sprintf("expected 404 after plugin deletion, got %d", proxyResp2.StatusCode))
	t.Logf("Proxy endpoint returns 404 after deletion - verified")
}
