package e2e_test

import (
	_ "embed"

	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/test/e2e/utils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const (
	// Default versions for Gateway API and agentgateway
	// Can be overridden via environment variables for testing different versions:
	//   GATEWAY_API_VERSION - Gateway API CRDs version (default: v1.4.0)
	//   AGENTGATEWAY_VERSION - agentgateway helm chart version (default: v2.2.0-main)
	defaultGatewayAPIVersion   = "v1.4.0"
	defaultAgentGatewayVersion = "v2.2.0-main"
)

// getGatewayAPIVersion returns the Gateway API version to use, checking env var first
func getGatewayAPIVersion() string {
	if v := os.Getenv("GATEWAY_API_VERSION"); v != "" {
		return v
	}
	return defaultGatewayAPIVersion
}

// getAgentGatewayVersion returns the agentgateway version to use, checking env var first
func getAgentGatewayVersion() string {
	if v := os.Getenv("AGENTGATEWAY_VERSION"); v != "" {
		return v
	}
	return defaultAgentGatewayVersion
}

//go:embed manifests/everything-mcp-server.yaml
var mcpServerManifest string

//go:embed manifests/proxy-test-resources.yaml
var proxyTestResources string

//go:embed manifests/proxy-deny-policy.yaml
var proxyDenyPolicy string

// setupProxyConfig adds proxy URL to controller ConfigMap and returns cleanup function
func setupProxyConfig(t *testing.T, cli client.Client, proxyURL string) func() {
	configMap := &corev1.ConfigMap{}
	err := cli.Get(t.Context(), client.ObjectKey{
		Name:      "kagent-controller",
		Namespace: "kagent",
	}, configMap)
	require.NoError(t, err)

	// Add proxy URL
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data["PROXY_URL"] = proxyURL

	err = cli.Update(t.Context(), configMap)
	require.NoError(t, err)

	// Restart the controller deployment to pick up the new configuration
	// The controller loads PROXY_URL from environment variables at startup
	err = utils.RunKubectl(t.Context(), "", "rollout", "restart", "deployment/kagent-controller", "-n", "kagent")
	require.NoError(t, err)

	// Wait for the rollout to complete
	err = utils.RunKubectl(t.Context(), "", "rollout", "status", "deployment/kagent-controller", "-n", "kagent", "--timeout=2m")
	require.NoError(t, err)

	// Re-establish port-forward since restarting the controller cancels any existing port-forward
	cleanupPortForward, err := utils.EnsurePortForward()
	require.NoError(t, err)

	// Return cleanup function
	return func() {
		// Cleanup port-forward
		cleanupPortForward()

		// Remove PROXY_URL from ConfigMap
		configMap := &corev1.ConfigMap{}
		err := cli.Get(context.Background(), client.ObjectKey{
			Name:      "kagent-controller",
			Namespace: "kagent",
		}, configMap)
		if err != nil {
			t.Logf("Failed to get ConfigMap for cleanup: %v", err)
			return
		}

		delete(configMap.Data, "PROXY_URL")

		err = cli.Update(context.Background(), configMap)
		if err != nil {
			t.Logf("Failed to remove PROXY_URL from ConfigMap: %v", err)
			return
		}

		// Restart the controller deployment to pick up the removal
		// The controller loads PROXY_URL from environment variables at startup
		t.Log("Restarting kagent-controller to remove proxy configuration...")
		err = utils.RunKubectl(context.Background(), "", "rollout", "restart", "deployment/kagent-controller", "-n", "kagent")
		if err != nil {
			t.Logf("Failed to restart controller during cleanup: %v", err)
			return
		}

		// Wait for the rollout to complete (with shorter timeout for cleanup)
		err = utils.RunKubectl(context.Background(), "", "rollout", "status", "deployment/kagent-controller", "-n", "kagent", "--timeout=1m")
		if err != nil {
			t.Logf("Warning: controller rollout may not have completed during cleanup: %v", err)
		}
	}
}

// installGatewayAPIPrerequisites installs Gateway API CRDs and agentgateway if not present
func installGatewayAPIPrerequisites(t *testing.T) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Check and install Gateway API CRDs
	if err := utils.RunKubectl(ctx, "", "get", "crd", "gateways.gateway.networking.k8s.io"); err != nil {
		t.Log("Gateway API CRDs not found, installing...")

		// Install standard Gateway API CRDs
		gatewayAPIURL := fmt.Sprintf("https://github.com/kubernetes-sigs/gateway-api/releases/download/%s/standard-install.yaml", getGatewayAPIVersion())
		if err := utils.RunKubectl(ctx, "", "apply", "-f", gatewayAPIURL); err != nil {
			return fmt.Errorf("failed to install Gateway API CRDs: %w", err)
		}

		// Wait for CRDs to be established
		if err := utils.RunKubectl(ctx, "", "wait", "--for=condition=Established",
			"--timeout=90s", "crd/gateways.gateway.networking.k8s.io"); err != nil {
			return fmt.Errorf("Gateway API CRDs not ready: %w", err)
		}

		if err := utils.RunKubectl(ctx, "", "wait", "--for=condition=Established",
			"--timeout=90s", "crd/httproutes.gateway.networking.k8s.io"); err != nil {
			return fmt.Errorf("Gateway API CRDs not ready: %w", err)
		}
	}

	// Check and install agentgateway
	if err := utils.RunKubectl(ctx, "", "get", "gatewayclass", "agentgateway"); err != nil {
		t.Log("agentgateway not found, installing...")

		// Install agentgateway CRDs
		t.Log("Installing agentgateway CRDs...")
		agentGatewayVersion := getAgentGatewayVersion()
		cmd := exec.CommandContext(ctx, "helm", "upgrade", "-i",
			"--create-namespace",
			"--namespace", "agentgateway-system",
			"--version", agentGatewayVersion,
			"agentgateway-crds",
			"oci://ghcr.io/kgateway-dev/charts/agentgateway-crds")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install agentgateway CRDs: %w", err)
		}

		// Install agentgateway
		t.Log("Installing agentgateway...")
		cmd = exec.CommandContext(ctx, "helm", "upgrade", "-i",
			"-n", "agentgateway-system",
			"--version", agentGatewayVersion,
			"agentgateway",
			"oci://ghcr.io/kgateway-dev/charts/agentgateway")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install agentgateway: %w", err)
		}

		// Wait for agentgateway pods to be ready
		t.Log("Waiting for agentgateway pods to be ready...")
		err = utils.Poll(ctx, "agentgateway pods to be ready", func() bool {
			return utils.RunKubectl(ctx, "", "get", "pods", "-l", "app.kubernetes.io/name=agentgateway",
				"-n", "agentgateway-system") == nil
		}, 2*time.Second)
		require.NoError(t, err, "Failed to wait for agentgateway pods to be ready")

		// Verify GatewayClass exists

		err = utils.Poll(ctx, "agentgateway GatewayClass", func() bool {
			return utils.RunKubectl(ctx, "", "get", "gatewayclass", "agentgateway") == nil
		}, 2*time.Second)
		require.NoError(t, err, "Failed to wait for agentgateway GatewayClass")
	}

	return nil
}

// uninstallGatewayAPIPrerequisites removes Gateway API CRDs and agentgateway
func uninstallGatewayAPIPrerequisites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Log("Cleaning up Gateway API prerequisites...")

	// Uninstall agentgateway
	cmd := exec.CommandContext(ctx, "helm", "uninstall", "agentgateway", "-n", "agentgateway-system")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: failed to uninstall agentgateway: %v", err)
	}

	// Uninstall agentgateway CRDs
	cmd = exec.CommandContext(ctx, "helm", "uninstall", "agentgateway-crds", "-n", "agentgateway-system")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: failed to uninstall agentgateway CRDs: %v", err)
	}

	// Delete agentgateway-system namespace
	if err := utils.RunKubectl(ctx, "", "delete", "namespace", "agentgateway-system", "--ignore-not-found=true", "--timeout=60s"); err != nil {
		t.Logf("Warning: failed to delete agentgateway-system namespace: %v", err)
	}

	// Delete Gateway API CRDs
	gatewayAPIURL := fmt.Sprintf("https://github.com/kubernetes-sigs/gateway-api/releases/download/%s/standard-install.yaml", getGatewayAPIVersion())
	if err := utils.RunKubectl(ctx, "", "delete", "-f", gatewayAPIURL, "--ignore-not-found=true"); err != nil {
		t.Logf("Warning: failed to delete Gateway API CRDs: %v", err)
	}
}

// setupProxyResources creates all static proxy test resources needed for the complete flow:
//   - Gateway: Receives proxied tool calls from agents
//   - HTTPRoutes: Route tool calls based on x-kagent-host header to backends
//   - MCPServer: Backend tool provider
//   - Service: Service-based tool provider (points to MCPServer)
//   - Agents: proxy-test-agent (makes tool calls) and target-agent (receives tool calls)
func setupProxyResources(t *testing.T) {
	// Apply MCPServer (shared resource)
	err := utils.RunKubectl(t.Context(), mcpServerManifest, "apply", "-f", "-")
	require.NoError(t, err, "Failed to create MCPServer")

	// Apply all proxy test resources
	err = utils.RunKubectl(t.Context(), proxyTestResources, "apply", "-f", "-")
	require.NoError(t, err, "Failed to create proxy test resources")

	// Cleanup all resources
	t.Cleanup(func() {
		ctx := context.Background()
		_ = utils.RunKubectl(ctx, proxyTestResources, "delete", "-f", "-")
		_ = utils.RunKubectl(ctx, mcpServerManifest, "delete", "-f", "-")
	})

	// Wait for agentgateway to create the Service for the Gateway
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	err = utils.Poll(ctx, "proxy Service", func() bool {
		return utils.RunKubectl(ctx, "", "get", "svc", "proxy", "-n", "kagent") == nil
	}, 2*time.Second)
	require.NoError(t, err, "Failed to wait for proxy Service")
}

// runSyncTestExpectFailure verifies that an A2A request fails
func runSyncTestExpectFailure(t *testing.T, a2aClient *a2aclient.A2AClient, userMessage string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	msg := protocol.Message{
		Kind:  protocol.KindMessage,
		Role:  protocol.MessageRoleUser,
		Parts: []protocol.Part{protocol.NewTextPart(userMessage)},
	}

	_, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{Message: msg})
	require.Error(t, err, "Expected request to fail but it succeeded")
}

// waitForPolicyEnforcement waits for an AgentGatewayPolicy to be accepted, attached, and enforced
// by the gateway data plane. It verifies enforcement by making a direct HTTP request to the proxy
// from within the cluster and checking for a 403 response.
func waitForPolicyEnforcement(t *testing.T, ctx context.Context, policyName string) {
	// Wait for the policy to be ACCEPTED and ATTACHED
	// The status conditions are under .status.ancestors[0].conditions
	err := utils.Poll(ctx, "policy to be accepted and attached", func() bool {
		cmd := exec.Command("kubectl", "get", "agentgatewaypolicy", policyName, "-n", "kagent",
			"-o", "jsonpath={.status.ancestors[0].conditions[?(@.type=='Accepted')].status} {.status.ancestors[0].conditions[?(@.type=='Attached')].status}")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false
		}
		status := strings.TrimSpace(string(output))
		return status == "True True"
	}, 1*time.Second)
	require.NoError(t, err, "Policy was not accepted and attached within timeout")

	// Wait for the policy to propagate to the gateway's data plane
	// We verify enforcement by making a direct HTTP request to the proxy from within the cluster
	// This avoids port-forwarding and should return 403 immediately when policy is enforced
	t.Log("Waiting for policy to be enforced by gateway data plane...")
	enforcementCtx, enforcementCancel := context.WithTimeout(ctx, 10*time.Second)
	defer enforcementCancel()

	// Get a pod we can use to exec curl from (use proxy-test-agent pod which we know exists)
	// This pod should have curl available and is already running
	agentPodCmd := exec.Command("kubectl", "get", "pods", "-n", "kagent",
		"-l", "app.kubernetes.io/name=proxy-test-agent",
		"-o", "jsonpath={.items[0].metadata.name}")
	agentPodOutput, err := agentPodCmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(agentPodOutput)) == "" {
		// Fallback: try to get any pod from kagent namespace
		fallbackCmd := exec.Command("kubectl", "get", "pods", "-n", "kagent",
			"--field-selector=status.phase=Running",
			"-o", "jsonpath={.items[0].metadata.name}")
		fallbackOutput, fallbackErr := fallbackCmd.CombinedOutput()
		if fallbackErr != nil || strings.TrimSpace(string(fallbackOutput)) == "" {
			require.NoError(t, err, "Failed to get pod for curl exec")
		}
		agentPodOutput = fallbackOutput
	}
	agentPodName := strings.TrimSpace(string(agentPodOutput))
	require.NotEmpty(t, agentPodName, "Pod name is empty - cannot exec curl")

	// Proxy service URL from within the cluster
	proxyURL := "http://proxy.kagent.svc.cluster.local:8080"

	// First, wait for initial 403 response
	err = utils.Poll(enforcementCtx, "policy enforcement to be active (checking for 403 response)", func() bool {
		// Make a direct HTTP request to the proxy with x-kagent-host header
		// This simulates a tool call that should be denied by the policy
		// Use curl from within the cluster to avoid port-forwarding
		curlCmd := exec.Command("kubectl", "exec", "-n", "kagent", agentPodName, "--",
			"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
			"-H", "x-kagent-host: everything-mcp-server.kagent",
			"-X", "POST",
			proxyURL+"/mcp")

		output, err := curlCmd.CombinedOutput()
		if err != nil {
			// Pod might not have curl, or exec failed
			return false
		}

		statusCode := strings.TrimSpace(string(output))
		if statusCode == "403" {
			t.Logf("Got 403 Forbidden from proxy - policy enforcement is active")
			return true
		}

		// Log other status codes for debugging
		if statusCode != "200" && statusCode != "" {
			t.Logf("Proxy returned HTTP %s (waiting for 403)", statusCode)
		}

		return false
	}, 500*time.Millisecond)
	require.NoError(t, err, "Policy enforcement did not become active within timeout (proxy did not return 403)")

	// Verify enforcement is stable by checking multiple times
	t.Log("Verifying policy enforcement is stable...")
	consecutiveSuccesses := 0
	requiredSuccesses := 3
	stabilityCtx, stabilityCancel := context.WithTimeout(ctx, 5*time.Second)
	defer stabilityCancel()

	err = utils.Poll(stabilityCtx, "policy enforcement to be stable (3 consecutive 403 responses)", func() bool {
		curlCmd := exec.Command("kubectl", "exec", "-n", "kagent", agentPodName, "--",
			"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
			"-H", "x-kagent-host: everything-mcp-server.kagent",
			"-X", "POST",
			proxyURL+"/mcp")

		output, err := curlCmd.CombinedOutput()
		if err == nil {
			statusCode := strings.TrimSpace(string(output))
			if statusCode == "403" {
				consecutiveSuccesses++
				if consecutiveSuccesses >= requiredSuccesses {
					t.Logf("Policy enforcement verified stable after %d consecutive 403 responses", consecutiveSuccesses)
					return true
				}
			} else {
				t.Logf("Warning: Expected 403 but got %s during stability check, resetting counter", statusCode)
				consecutiveSuccesses = 0
			}
		}
		return false
	}, 200*time.Millisecond)
	require.NoError(t, err, "Policy enforcement is not stable - not getting consistent 403 responses")

	// Add a small delay to ensure any existing agent connections are affected by the policy
	t.Log("Allowing time for existing connections to be affected by policy...")
	time.Sleep(1 * time.Second)
}

// TestE2EProxyConfiguration validates that all agent tool calls correctly route through
// the configured proxy gateway.:
//
//  1. User sends message to controller at /api/a2a/{namespace}/{agent}
//  2. Controller routes message to the agent (proxy-test-agent)
//  3. Agent makes tool calls which are routed through the proxy gateway
//  4. Proxy gateway routes to backends based on x-kagent-host header
//  5. Proxy allows or denies the tool call based on the deny policy
func TestE2EProxyConfiguration(t *testing.T) {
	// Check prerequisites
	if err := installGatewayAPIPrerequisites(t); err != nil {
		t.Fatalf("Failed to install prerequisites: %v", err)
	}

	// Schedule cleanup of prerequisites
	t.Cleanup(func() {
		uninstallGatewayAPIPrerequisites(t)
	})

	// Setup Kubernetes client
	cli := setupK8sClient(t, true)

	// Setup mock LLM server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_proxy_test.json")
	defer stopServer()

	// Setup model config with fixed name (referenced by agent in manifest)
	modelCfg := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proxy-test-model-config",
			Namespace: "kagent",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4.1-mini",
			APIKeySecret:    "kagent-openai",
			APIKeySecretKey: "OPENAI_API_KEY",
			Provider:        v1alpha2.ModelProviderOpenAI,
			OpenAI: &v1alpha2.OpenAIConfig{
				BaseURL: baseURL + "/v1",
			},
		},
	}
	err := cli.Create(t.Context(), modelCfg)
	require.NoError(t, err, "Failed to create model config")
	cleanup(t, cli, modelCfg)

	// Setup all static proxy resources (Gateway, MCPServer, Service, Agent, HTTPRoutes)
	setupProxyResources(t)

	// Configure proxy URL in controller (before agents start)
	cleanupProxy := setupProxyConfig(t, cli, "http://proxy.kagent.svc.cluster.local:8080")
	defer cleanupProxy()

	// Wait for both agents to be ready
	err = utils.Poll(t.Context(), "target-agent-proxy-test", func() bool {
		return utils.RunKubectl(t.Context(), "", "get", "agents.kagent.dev", "target-agent-proxy-test", "-n", "kagent") == nil
	}, 2*time.Second)
	require.NoError(t, err, "Failed to wait for target-agent-proxy-test")

	err = utils.Poll(t.Context(), "proxy-test-agent", func() bool {
		return utils.RunKubectl(t.Context(), "", "get", "agents.kagent.dev", "proxy-test-agent", "-n", "kagent") == nil
	}, 2*time.Second)
	require.NoError(t, err, "Failed to wait for proxy-test-agent")
	t.Log("Main agent is ready")

	// Setup A2A client to communicate with agent through the controller
	// URL format: http://localhost:8083/api/a2a/{namespace}/{agent}
	// This ensures we test the complete user -> controller -> agent -> proxy -> backend flow
	a2aURL := a2aUrl("kagent", "proxy-test-agent")
	a2aClient, err := a2aclient.NewA2AClient(a2aURL)
	require.NoError(t, err)

	// Test 1: Agent-to-agent communication through proxy
	// Flow: User -> Controller -> proxy-test-agent -> Proxy Gateway -> target-agent-proxy-test
	t.Run("agent_to_agent", func(t *testing.T) {
		t.Log("Testing agent-to-agent communication through proxy...")
		runSyncTest(t, a2aClient, "call the target agent", "target agent response", nil)
	})

	// Test 2: MCPServer resource through proxy
	// Flow: User -> Controller -> proxy-test-agent -> Proxy Gateway -> everything-mcp-server
	t.Run("mcpserver_resource", func(t *testing.T) {
		t.Log("Testing MCPServer resource through proxy...")
		runSyncTest(t, a2aClient, "use the mcp server to add 10 and 20", "30", nil)
	})

	// Test 3: Service as MCP Tool through proxy
	// Flow: User -> Controller -> proxy-test-agent -> Proxy Gateway -> test-mcp-service
	t.Run("service_as_mcp_tool", func(t *testing.T) {
		t.Log("Testing service as MCP tool through proxy...")
		// The service points to the MCPServer backend, so we can use the add tool
		runSyncTest(t, a2aClient, "use the service tool to add 5 and 7", "12", nil)
	})

	// Test 4: Apply deny policy and verify all routes are blocked
	// This tests that the proxy gateway correctly enforces policies on tool calls
	t.Run("deny_policy", func(t *testing.T) {
		t.Log("Applying deny policy to Gateway...")
		t.Log("This will block all tool calls through the proxy while allowing user->agent communication")
		err := utils.RunKubectl(t.Context(), proxyDenyPolicy, "apply", "-f", "-")
		require.NoError(t, err, "Failed to apply deny policy")

		// Cleanup policy
		t.Cleanup(func() {
			ctx := context.Background()
			_ = utils.RunKubectl(ctx, proxyDenyPolicy, "delete", "-f", "-")
		})

		// Wait for policy to be created and propagate
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()

		// First wait for the policy resource to exist
		err = utils.Poll(ctx, "AgentgatewayPolicy proxy-deny-test", func() bool {
			return utils.RunKubectl(ctx, "", "get", "agentgatewaypolicy", "proxy-deny-test", "-n", "kagent") == nil
		}, 2*time.Second)
		require.NoError(t, err, "Failed to wait for deny policy")

		// Wait for policy to be accepted, attached, and enforced by the gateway
		waitForPolicyEnforcement(t, ctx, "proxy-deny-test")

		// Verify all previously working routes are now denied
		// Note: User can still reach proxy-test-agent, but the agent's tool calls will fail at the proxy
		t.Run("agent_to_agent_denied", func(t *testing.T) {
			t.Log("Verifying agent-to-agent tool call is denied at proxy...")
			runSyncTestExpectFailure(t, a2aClient, "call the target agent")
		})

		t.Run("mcpserver_resource_denied", func(t *testing.T) {
			t.Log("Verifying MCPServer tool call is denied at proxy...")
			runSyncTestExpectFailure(t, a2aClient, "use the mcp server to add 10 and 20")
		})

		t.Run("service_as_mcp_tool_denied", func(t *testing.T) {
			t.Log("Verifying service tool call is denied at proxy...")
			runSyncTestExpectFailure(t, a2aClient, "use the service tool to add 5 and 7")
		})
	})
}
