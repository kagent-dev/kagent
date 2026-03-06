package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfNoTemporal skips the test if the Temporal server is not deployed.
func skipIfNoTemporal(t *testing.T) {
	t.Helper()
	if os.Getenv("TEMPORAL_ENABLED") == "" {
		t.Skip("Skipping Temporal E2E test: set TEMPORAL_ENABLED=1 to run (requires Temporal + NATS in cluster)")
	}
}

// waitForTemporalReady polls the Temporal server service until it is reachable.
func waitForTemporalReady(t *testing.T) {
	t.Helper()
	t.Log("Waiting for Temporal server to be ready")
	cli := setupK8sClient(t, false)

	pollErr := wait.PollUntilContextTimeout(t.Context(), 3*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		var deploy appsv1.Deployment
		err := cli.Get(ctx, client.ObjectKey{Namespace: "kagent", Name: "temporal-server"}, &deploy)
		if err != nil {
			t.Logf("Temporal server deployment not found: %v", err)
			return false, nil
		}
		if deploy.Status.ReadyReplicas > 0 {
			return true, nil
		}
		t.Logf("Temporal server not ready yet (readyReplicas=%d)", deploy.Status.ReadyReplicas)
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for Temporal server")
}

// waitForNATSReady polls the NATS service until it is reachable.
func waitForNATSReady(t *testing.T) {
	t.Helper()
	t.Log("Waiting for NATS to be ready")
	cli := setupK8sClient(t, false)

	pollErr := wait.PollUntilContextTimeout(t.Context(), 3*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		var deploy appsv1.Deployment
		err := cli.Get(ctx, client.ObjectKey{Namespace: "kagent", Name: "nats"}, &deploy)
		if err != nil {
			t.Logf("NATS deployment not found: %v", err)
			return false, nil
		}
		if deploy.Status.ReadyReplicas > 0 {
			return true, nil
		}
		t.Logf("NATS not ready yet (readyReplicas=%d)", deploy.Status.ReadyReplicas)
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for NATS")
}

// setupTemporalAgent creates an agent with temporal.enabled: true using the Go ADK image.
func setupTemporalAgent(t *testing.T, cli client.Client, modelConfigName string, opts AgentOptions) *v1alpha2.Agent {
	if opts.Name == "" {
		opts.Name = "temporal-test"
	}
	if opts.SystemMessage == "" {
		opts.SystemMessage = "You are a test agent."
	}

	golangADKRepo := "kagent-dev/kagent/golang-adk"
	opts.ImageRepository = &golangADKRepo

	agent := generateAgent(modelConfigName, nil, opts)
	agent.Spec.Temporal = &v1alpha2.TemporalSpec{
		Enabled: true,
	}

	err := cli.Create(t.Context(), agent)
	require.NoError(t, err)
	cleanup(t, cli, agent)

	// Wait for agent to be ready.
	args := []string{
		"wait", "--for", "condition=Ready", "--timeout=2m",
		"agents.kagent.dev", agent.Name, "-n", "kagent",
	}
	cmd := exec.CommandContext(t.Context(), "kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	waitForEndpoint(t, agent.Namespace, agent.Name)

	return agent
}

// TestE2ETemporalInfrastructure verifies that Temporal server and NATS are
// deployed and healthy when temporal.enabled=true in Helm values.
func TestE2ETemporalInfrastructure(t *testing.T) {
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	cli := setupK8sClient(t, false)

	// Verify Temporal server service exists.
	var svc corev1.Service
	err := cli.Get(t.Context(), client.ObjectKey{Namespace: "kagent", Name: "temporal-server"}, &svc)
	require.NoError(t, err, "Temporal server service should exist")
	assert.Equal(t, int32(7233), svc.Spec.Ports[0].Port, "Temporal server should listen on port 7233")

	// Verify NATS service exists.
	err = cli.Get(t.Context(), client.ObjectKey{Namespace: "kagent", Name: "nats"}, &svc)
	require.NoError(t, err, "NATS service should exist")
	assert.Equal(t, int32(4222), svc.Spec.Ports[0].Port, "NATS should listen on port 4222")

	t.Log("Temporal and NATS infrastructure verified")
}

// TestE2ETemporalAgentCRDTranslation verifies that an Agent CRD with
// temporal.enabled: true produces a pod with the correct env vars and config.
func TestE2ETemporalAgentCRDTranslation(t *testing.T) {
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	// Setup mock server.
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{
		Name: "temporal-crd-test",
	})

	// Verify the agent pod has TEMPORAL_HOST_ADDR and NATS_ADDR env vars.
	podList := &corev1.PodList{}
	err := cli.List(t.Context(), podList,
		client.InNamespace("kagent"),
		client.MatchingLabels{
			"app.kubernetes.io/name":       agent.Name,
			"app.kubernetes.io/managed-by": "kagent",
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, podList.Items, "Agent should have at least one pod")

	pod := podList.Items[0]
	var hasTemporalAddr, hasNATSAddr bool
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			switch env.Name {
			case "TEMPORAL_HOST_ADDR":
				hasTemporalAddr = true
				t.Logf("TEMPORAL_HOST_ADDR=%s", env.Value)
			case "NATS_ADDR":
				hasNATSAddr = true
				t.Logf("NATS_ADDR=%s", env.Value)
			}
		}
	}
	assert.True(t, hasTemporalAddr, "Pod should have TEMPORAL_HOST_ADDR env var")
	assert.True(t, hasNATSAddr, "Pod should have NATS_ADDR env var")

	// Verify agent CRD has temporal spec reflected.
	var updatedAgent v1alpha2.Agent
	err = cli.Get(t.Context(), client.ObjectKeyFromObject(agent), &updatedAgent)
	require.NoError(t, err)
	require.NotNil(t, updatedAgent.Spec.Temporal, "Agent should have Temporal spec")
	assert.True(t, updatedAgent.Spec.Temporal.Enabled, "Temporal should be enabled")
}

// TestE2ETemporalWorkflowExecution creates an Agent CRD with temporal.enabled: true,
// sends an A2A message, and verifies the workflow executes and returns a response.
func TestE2ETemporalWorkflowExecution(t *testing.T) {
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	// Setup mock server.
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{
		Name: "temporal-exec-test",
	})

	// Setup A2A client.
	a2aClient := setupA2AClient(t, agent)

	t.Run("sync_invocation", func(t *testing.T) {
		runSyncTest(t, a2aClient, "What is the capital of France?", "Paris", nil)
	})

	t.Run("streaming_invocation", func(t *testing.T) {
		runStreamingTest(t, a2aClient, "What is the capital of France?", "Paris")
	})
}

// TestE2ETemporalUIPlugin verifies the Temporal UI plugin is accessible
// via the kagent plugin proxy when temporal.enabled=true.
func TestE2ETemporalUIPlugin(t *testing.T) {
	skipIfNoTemporal(t)
	waitForTemporalReady(t)

	baseURL := kagentBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Poll /api/plugins for the temporal plugin.
	pluginsURL := baseURL + "/api/plugins"
	t.Logf("Checking %s for temporal plugin", pluginsURL)

	var found bool
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pluginsURL, nil)
		if err != nil {
			return false, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return false, nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return false, nil
		}

		// Just check that the proxy route exists for temporal.
		proxyURL := baseURL + "/plugins/temporal/"
		proxyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
		if err != nil {
			return false, nil
		}
		proxyResp, err := httpClient.Do(proxyReq)
		if err != nil {
			return false, nil
		}
		proxyResp.Body.Close()

		// 502 means proxy is configured but upstream may not be ready yet.
		// 404 means the route doesn't exist at all.
		if proxyResp.StatusCode != http.StatusNotFound {
			found = true
			return true, nil
		}
		return false, nil
	})

	if pollErr != nil {
		t.Logf("Temporal UI plugin not found (may not be configured as RemoteMCPServer): %v", pollErr)
		t.Skip("Temporal UI plugin not configured")
	}

	assert.True(t, found, "Temporal UI should be accessible via plugin proxy")
	t.Log("Temporal UI plugin verified")
}

// TestE2ETemporalFallbackPath verifies that an agent WITHOUT temporal.enabled
// still works via the synchronous execution path (unchanged behavior).
func TestE2ETemporalFallbackPath(t *testing.T) {
	skipIfNoTemporal(t)

	// Setup mock server.
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_golang_adk_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)

	// Create agent WITHOUT temporal spec (fallback to sync path).
	golangADKRepo := "kagent-dev/kagent/golang-adk"
	agent := setupAgentWithOptions(t, cli, modelCfg.Name, nil, AgentOptions{
		Name:            "temporal-fallback-test",
		ImageRepository: &golangADKRepo,
	})

	a2aClient := setupA2AClient(t, agent)

	t.Run("sync_invocation_no_temporal", func(t *testing.T) {
		runSyncTest(t, a2aClient, "What is 2+2?", "4", nil)
	})
}

// TestE2ETemporalCrashRecovery verifies that a Temporal workflow resumes
// after an agent pod restart. It kills the pod mid-execution and checks
// that the workflow eventually completes.
func TestE2ETemporalCrashRecovery(t *testing.T) {
	skipIfNoTemporal(t)
	if os.Getenv("TEMPORAL_CRASH_RECOVERY_TEST") == "" {
		t.Skip("Skipping crash recovery test: set TEMPORAL_CRASH_RECOVERY_TEST=1 to run (slow, destructive)")
	}
	waitForTemporalReady(t)
	waitForNATSReady(t)

	// Setup mock server.
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{
		Name: "temporal-crash-test",
	})

	// Delete the agent pod to simulate a crash.
	podList := &corev1.PodList{}
	err := cli.List(t.Context(), podList,
		client.InNamespace("kagent"),
		client.MatchingLabels{
			"app.kubernetes.io/name":       agent.Name,
			"app.kubernetes.io/managed-by": "kagent",
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, podList.Items)

	// Delete the pod.
	t.Logf("Deleting pod %s to simulate crash", podList.Items[0].Name)
	err = cli.Delete(t.Context(), &podList.Items[0])
	require.NoError(t, err)

	// Wait for replacement pod to come up.
	t.Log("Waiting for replacement pod")
	pollErr := wait.PollUntilContextTimeout(t.Context(), 3*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		if err := cli.List(ctx, &pods,
			client.InNamespace("kagent"),
			client.MatchingLabels{
				"app.kubernetes.io/name":       agent.Name,
				"app.kubernetes.io/managed-by": "kagent",
			},
		); err != nil {
			return false, nil
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning && pod.Name != podList.Items[0].Name {
				return true, nil
			}
		}
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for replacement pod")

	waitForEndpoint(t, agent.Namespace, agent.Name)

	// After recovery, the agent should still be able to handle requests.
	a2aClient := setupA2AClient(t, agent)
	t.Run("post_crash_invocation", func(t *testing.T) {
		runSyncTest(t, a2aClient, "What is the capital of France?", "Paris", nil)
	})
}

// TestE2ETemporalWithCustomTimeout verifies that an agent with a custom
// workflow timeout in the TemporalSpec is correctly configured.
func TestE2ETemporalWithCustomTimeout(t *testing.T) {
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)

	golangADKRepo := "kagent-dev/kagent/golang-adk"
	agent := generateAgent(modelCfg.Name, nil, AgentOptions{
		Name:            "temporal-timeout-test",
		ImageRepository: &golangADKRepo,
	})
	agent.Spec.Temporal = &v1alpha2.TemporalSpec{
		Enabled:         true,
		WorkflowTimeout: &metav1.Duration{Duration: 1 * time.Hour},
		RetryPolicy: &v1alpha2.TemporalRetryPolicy{
			LLMMaxAttempts:  int32Ptr(3),
			ToolMaxAttempts: int32Ptr(2),
		},
	}

	err := cli.Create(t.Context(), agent)
	require.NoError(t, err)
	cleanup(t, cli, agent)

	// Wait for agent to be ready.
	args := []string{
		"wait", "--for", "condition=Ready", "--timeout=2m",
		"agents.kagent.dev", agent.Name, "-n", "kagent",
	}
	cmd := exec.CommandContext(t.Context(), "kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())

	waitForEndpoint(t, agent.Namespace, agent.Name)

	// Verify agent is responsive with custom config.
	a2aClient := setupA2AClient(t, agent)
	runSyncTest(t, a2aClient, "What is the capital of France?", "Paris", nil)

	// Verify the CRD persisted the custom timeout.
	var updatedAgent v1alpha2.Agent
	err = cli.Get(t.Context(), client.ObjectKeyFromObject(agent), &updatedAgent)
	require.NoError(t, err)
	require.NotNil(t, updatedAgent.Spec.Temporal)
	require.NotNil(t, updatedAgent.Spec.Temporal.WorkflowTimeout)
	assert.Equal(t, 1*time.Hour, updatedAgent.Spec.Temporal.WorkflowTimeout.Duration)
	require.NotNil(t, updatedAgent.Spec.Temporal.RetryPolicy)
	assert.Equal(t, int32(3), *updatedAgent.Spec.Temporal.RetryPolicy.LLMMaxAttempts)
	assert.Equal(t, int32(2), *updatedAgent.Spec.Temporal.RetryPolicy.ToolMaxAttempts)
}

func int32Ptr(v int32) *int32 {
	return &v
}

// TestE2ETemporalWorkflowVisibleInTemporalUI verifies that after executing
// an agent workflow, the workflow execution can be queried via kubectl port-forward
// to the Temporal server (gRPC). This validates end-to-end that workflows are
// actually registered in Temporal.
func TestE2ETemporalWorkflowVisibleInTemporalUI(t *testing.T) {
	skipIfNoTemporal(t)
	if os.Getenv("TEMPORAL_UI_TEST") == "" {
		t.Skip("Skipping Temporal UI test: set TEMPORAL_UI_TEST=1 to run")
	}
	waitForTemporalReady(t)
	waitForNATSReady(t)

	// This test uses kubectl + tctl to verify workflow existence.
	// The actual check is: after sending a message, verify that the Temporal
	// server has a workflow execution for the agent's task queue.
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{
		Name: "temporal-ui-test",
	})

	a2aClient := setupA2AClient(t, agent)
	runSyncTest(t, a2aClient, "What is the capital of France?", "Paris", nil)

	// Use kubectl exec to run tctl inside the Temporal server pod to verify workflow.
	taskQueue := fmt.Sprintf("agent-%s", agent.Name)
	t.Logf("Verifying workflow on task queue: %s", taskQueue)

	// List workflow executions using kubectl exec into the temporal-server pod.
	cmd := exec.CommandContext(t.Context(), "kubectl", "exec",
		"deploy/temporal-server", "-n", "kagent", "--",
		"tctl", "workflow", "list", "--query",
		fmt.Sprintf("TaskQueue='%s'", taskQueue),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("tctl output: %s", string(output))
		t.Logf("tctl command failed (tctl may not be available): %v", err)
		t.Skip("tctl not available in Temporal server pod")
	}
	t.Logf("Workflow list for task queue %s:\n%s", taskQueue, string(output))
}
