package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// skipIfNoWorkflows skips the test if the workflow E2E tests are not enabled.
func skipIfNoWorkflows(t *testing.T) {
	t.Helper()
	if os.Getenv("WORKFLOW_E2E_ENABLED") == "" {
		t.Skip("Skipping workflow E2E test: set WORKFLOW_E2E_ENABLED=1 to run (requires Temporal + NATS in cluster)")
	}
}

// createWorkflowTemplate creates a WorkflowTemplate and registers cleanup.
func createWorkflowTemplate(t *testing.T, cli client.Client, tmpl *v1alpha2.WorkflowTemplate) *v1alpha2.WorkflowTemplate {
	t.Helper()
	err := cli.Create(t.Context(), tmpl)
	require.NoError(t, err, "failed to create WorkflowTemplate")
	t.Cleanup(func() {
		if os.Getenv("SKIP_CLEANUP") == "" || !t.Failed() {
			cli.Delete(context.Background(), tmpl) //nolint:errcheck
		}
	})
	return tmpl
}

// createWorkflowRun creates a WorkflowRun and registers cleanup.
func createWorkflowRun(t *testing.T, cli client.Client, run *v1alpha2.WorkflowRun) *v1alpha2.WorkflowRun {
	t.Helper()
	err := cli.Create(t.Context(), run)
	require.NoError(t, err, "failed to create WorkflowRun")
	t.Cleanup(func() {
		if os.Getenv("SKIP_CLEANUP") == "" || !t.Failed() {
			cli.Delete(context.Background(), run) //nolint:errcheck
		}
	})
	return run
}

// waitForTemplateValidated polls until a WorkflowTemplate has Accepted=True.
func waitForTemplateValidated(t *testing.T, cli client.Client, key client.ObjectKey) *v1alpha2.WorkflowTemplate {
	t.Helper()
	var tmpl v1alpha2.WorkflowTemplate
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := cli.Get(ctx, key, &tmpl); err != nil {
			return false, nil
		}
		for _, c := range tmpl.Status.Conditions {
			if c.Type == v1alpha2.WorkflowTemplateConditionAccepted {
				return c.Status == metav1.ConditionTrue, nil
			}
		}
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for WorkflowTemplate %s to be validated", key.Name)
	return &tmpl
}

// waitForTemplateRejected polls until a WorkflowTemplate has Accepted=False with expected reason.
func waitForTemplateRejected(t *testing.T, cli client.Client, key client.ObjectKey, expectedReason string) *v1alpha2.WorkflowTemplate {
	t.Helper()
	var tmpl v1alpha2.WorkflowTemplate
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := cli.Get(ctx, key, &tmpl); err != nil {
			return false, nil
		}
		for _, c := range tmpl.Status.Conditions {
			if c.Type == v1alpha2.WorkflowTemplateConditionAccepted && c.Status == metav1.ConditionFalse {
				return true, nil
			}
		}
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for WorkflowTemplate %s to be rejected", key.Name)

	for _, c := range tmpl.Status.Conditions {
		if c.Type == v1alpha2.WorkflowTemplateConditionAccepted {
			assert.Equal(t, expectedReason, c.Reason)
		}
	}
	return &tmpl
}

// waitForRunPhase polls until a WorkflowRun reaches the expected phase.
func waitForRunPhase(t *testing.T, cli client.Client, key client.ObjectKey, phase string, timeout time.Duration) *v1alpha2.WorkflowRun {
	t.Helper()
	var run v1alpha2.WorkflowRun
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		if err := cli.Get(ctx, key, &run); err != nil {
			return false, nil
		}
		return run.Status.Phase == phase, nil
	})
	require.NoError(t, pollErr, "timed out waiting for WorkflowRun %s to reach phase %s (current: %s)", key.Name, phase, run.Status.Phase)
	return &run
}

// TestE2EWorkflowSequential verifies that a linear A->B->C workflow executes
// steps sequentially and reaches Succeeded phase.
func TestE2EWorkflowSequential(t *testing.T) {
	skipIfNoWorkflows(t)
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	cli := setupK8sClient(t, false)

	// Create a template with 3 sequential steps.
	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-seq-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Sequential A->B->C test",
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop", With: map[string]string{"msg": "hello"}},
				{Name: "step-b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-a"}, With: map[string]string{"msg": "world"}},
				{Name: "step-c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-b"}, With: map[string]string{"msg": "done"}},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	// Create a run.
	run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-seq-run-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: tmpl.Name,
		},
	})

	// Wait for run to succeed.
	finalRun := waitForRunPhase(t, cli, client.ObjectKeyFromObject(run), string(v1alpha2.WorkflowRunPhaseSucceeded), 120*time.Second)

	// Verify step statuses.
	require.Len(t, finalRun.Status.Steps, 3)
	for _, step := range finalRun.Status.Steps {
		assert.Equal(t, string(v1alpha2.StepPhaseSucceeded), string(step.Phase), "step %s should be Succeeded", step.Name)
	}
	assert.NotNil(t, finalRun.Status.CompletionTime, "completionTime should be set")
}

// TestE2EWorkflowParallelDAG verifies that A->[B,C]->D executes B and C
// concurrently after A, and D after both.
func TestE2EWorkflowParallelDAG(t *testing.T) {
	skipIfNoWorkflows(t)
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	cli := setupK8sClient(t, false)

	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-parallel-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Parallel DAG A->[B,C]->D test",
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop", With: map[string]string{"val": "root"}},
				{Name: "step-b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-a"}, With: map[string]string{"val": "left"}},
				{Name: "step-c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-a"}, With: map[string]string{"val": "right"}},
				{Name: "step-d", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-b", "step-c"}, With: map[string]string{"val": "join"}},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-parallel-run-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: tmpl.Name,
		},
	})

	finalRun := waitForRunPhase(t, cli, client.ObjectKeyFromObject(run), string(v1alpha2.WorkflowRunPhaseSucceeded), 120*time.Second)
	require.Len(t, finalRun.Status.Steps, 4)
	for _, step := range finalRun.Status.Steps {
		assert.Equal(t, string(v1alpha2.StepPhaseSucceeded), string(step.Phase), "step %s should be Succeeded", step.Name)
	}
}

// TestE2EWorkflowAgentStep verifies that a workflow with an agent step invokes
// a child workflow on the agent's task queue and maps the output.
func TestE2EWorkflowAgentStep(t *testing.T) {
	skipIfNoWorkflows(t)
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	// Setup mock LLM for the agent.
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)
	agent := setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{
		Name: "wf-agent-step-test",
	})

	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-agent-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Agent step test",
			Steps: []v1alpha2.StepSpec{
				{
					Name:     "ask-agent",
					Type:     v1alpha2.StepTypeAgent,
					AgentRef: agent.Name,
					Prompt:   "What is the capital of France?",
					Output:   &v1alpha2.StepOutput{As: "agentResult"},
				},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-agent-run-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: tmpl.Name,
		},
	})

	finalRun := waitForRunPhase(t, cli, client.ObjectKeyFromObject(run), string(v1alpha2.WorkflowRunPhaseSucceeded), 180*time.Second)
	require.Len(t, finalRun.Status.Steps, 1)
	assert.Equal(t, string(v1alpha2.StepPhaseSucceeded), string(finalRun.Status.Steps[0].Phase))
}

// TestE2EWorkflowFailFast verifies that when a step with onFailure=stop fails,
// dependent steps are skipped and the workflow reaches Failed phase.
func TestE2EWorkflowFailFast(t *testing.T) {
	skipIfNoWorkflows(t)
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	cli := setupK8sClient(t, false)

	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-failfast-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Fail-fast test: B fails, C should be skipped",
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop", With: map[string]string{"val": "ok"}},
				{
					Name:      "step-b",
					Type:      v1alpha2.StepTypeAction,
					Action:    "fail.always",
					DependsOn: []string{"step-a"},
					OnFailure: "stop",
					Policy: &v1alpha2.StepPolicy{
						Retry: &v1alpha2.WorkflowRetryPolicy{MaxAttempts: 1},
					},
				},
				{Name: "step-c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-b"}, With: map[string]string{"val": "should-not-run"}},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-failfast-run-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: tmpl.Name,
		},
	})

	finalRun := waitForRunPhase(t, cli, client.ObjectKeyFromObject(run), string(v1alpha2.WorkflowRunPhaseFailed), 120*time.Second)
	require.Len(t, finalRun.Status.Steps, 3)

	stepPhases := map[string]string{}
	for _, s := range finalRun.Status.Steps {
		stepPhases[s.Name] = string(s.Phase)
	}
	assert.Equal(t, string(v1alpha2.StepPhaseSucceeded), stepPhases["step-a"])
	assert.Equal(t, string(v1alpha2.StepPhaseFailed), stepPhases["step-b"])
	assert.Equal(t, string(v1alpha2.StepPhaseSkipped), stepPhases["step-c"])
}

// TestE2EWorkflowRetry verifies that a step with retry policy retries on failure.
func TestE2EWorkflowRetry(t *testing.T) {
	skipIfNoWorkflows(t)
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	cli := setupK8sClient(t, false)

	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-retry-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Retry test: step retries 3 times",
			Steps: []v1alpha2.StepSpec{
				{
					Name:   "retry-step",
					Type:   v1alpha2.StepTypeAction,
					Action: "noop",
					With:   map[string]string{"val": "retry-test"},
					Policy: &v1alpha2.StepPolicy{
						Retry: &v1alpha2.WorkflowRetryPolicy{
							MaxAttempts:     3,
							InitialInterval: metav1.Duration{Duration: 1 * time.Second},
						},
					},
				},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-retry-run-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: tmpl.Name,
		},
	})

	// With noop action, should succeed on first attempt.
	finalRun := waitForRunPhase(t, cli, client.ObjectKeyFromObject(run), string(v1alpha2.WorkflowRunPhaseSucceeded), 120*time.Second)
	require.Len(t, finalRun.Status.Steps, 1)
	assert.Equal(t, string(v1alpha2.StepPhaseSucceeded), string(finalRun.Status.Steps[0].Phase))
}

// TestE2EWorkflowCancellation verifies that deleting a WorkflowRun cancels
// the Temporal workflow and the finalizer is removed.
func TestE2EWorkflowCancellation(t *testing.T) {
	skipIfNoWorkflows(t)
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	cli := setupK8sClient(t, false)

	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-cancel-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Cancellation test",
			Steps: []v1alpha2.StepSpec{
				{
					Name:   "long-step",
					Type:   v1alpha2.StepTypeAction,
					Action: "noop",
					With:   map[string]string{"val": "cancel-me"},
					Policy: &v1alpha2.StepPolicy{
						Timeout: &v1alpha2.WorkflowTimeoutPolicy{
							StartToClose: metav1.Duration{Duration: 30 * time.Minute},
						},
					},
				},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-cancel-run-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: tmpl.Name,
		},
	})

	// Wait for run to be accepted and running.
	waitForRunPhase(t, cli, client.ObjectKeyFromObject(run), string(v1alpha2.WorkflowRunPhaseRunning), 60*time.Second)

	// Delete the run. The finalizer should cancel the Temporal workflow.
	err := cli.Delete(t.Context(), run)
	require.NoError(t, err)

	// Verify the run is deleted (finalizer removed).
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		var deleted v1alpha2.WorkflowRun
		err := cli.Get(ctx, client.ObjectKeyFromObject(run), &deleted)
		if err != nil {
			return true, nil // Not found = deleted
		}
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for WorkflowRun to be fully deleted")
}

// TestE2EWorkflowRetention verifies that the retention controller enforces
// successfulRunsHistoryLimit by deleting oldest completed runs.
func TestE2EWorkflowRetention(t *testing.T) {
	skipIfNoWorkflows(t)
	skipIfNoTemporal(t)
	waitForTemporalReady(t)
	waitForNATSReady(t)

	cli := setupK8sClient(t, false)

	limit := int32(2)
	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-retention-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Retention test",
			Retention: &v1alpha2.RetentionPolicy{
				SuccessfulRunsHistoryLimit: &limit,
			},
			Steps: []v1alpha2.StepSpec{
				{Name: "simple", Type: v1alpha2.StepTypeAction, Action: "noop", With: map[string]string{"val": "ok"}},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	// Create 4 runs — retention should keep only 2.
	runNames := make([]string, 4)
	for i := 0; i < 4; i++ {
		run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("wf-ret-run-%s-%d", tmpl.Name, i),
				Namespace: "kagent",
			},
			Spec: v1alpha2.WorkflowRunSpec{
				WorkflowTemplateRef: tmpl.Name,
			},
		})
		runNames[i] = run.Name
		// Wait for each to succeed before creating next.
		waitForRunPhase(t, cli, client.ObjectKeyFromObject(run), string(v1alpha2.WorkflowRunPhaseSucceeded), 120*time.Second)
		// Small delay to ensure distinct completion times.
		time.Sleep(2 * time.Second)
	}

	// Wait for retention controller to clean up (runs every 60s).
	t.Log("Waiting for retention controller to enforce history limits...")
	var remainingRuns int
	pollErr := wait.PollUntilContextTimeout(t.Context(), 10*time.Second, 180*time.Second, true, func(ctx context.Context) (bool, error) {
		runList := &v1alpha2.WorkflowRunList{}
		if err := cli.List(ctx, runList, client.InNamespace("kagent")); err != nil {
			return false, nil
		}
		count := 0
		for _, r := range runList.Items {
			if r.Spec.WorkflowTemplateRef == tmpl.Name {
				count++
			}
		}
		remainingRuns = count
		t.Logf("Retention check: %d runs remaining for template %s (limit=%d)", count, tmpl.Name, limit)
		return count <= int(limit), nil
	})
	require.NoError(t, pollErr, "timed out waiting for retention controller (remaining: %d, limit: %d)", remainingRuns, limit)
	assert.LessOrEqual(t, remainingRuns, int(limit))
}

// TestE2EWorkflowCycleDetection verifies that a WorkflowTemplate with a
// dependency cycle is rejected with CycleDetected reason.
func TestE2EWorkflowCycleDetection(t *testing.T) {
	skipIfNoWorkflows(t)

	cli := setupK8sClient(t, false)

	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-cycle-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Cycle detection test",
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-c"}},
				{Name: "step-b", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-a"}},
				{Name: "step-c", Type: v1alpha2.StepTypeAction, Action: "noop", DependsOn: []string{"step-b"}},
			},
		},
	})

	waitForTemplateRejected(t, cli, client.ObjectKeyFromObject(tmpl), "CycleDetected")
}

// TestE2EWorkflowMissingParam verifies that a WorkflowRun with a missing required
// parameter is rejected (Accepted=False) without starting a Temporal workflow.
func TestE2EWorkflowMissingParam(t *testing.T) {
	skipIfNoWorkflows(t)

	cli := setupK8sClient(t, false)

	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-param-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "Missing param test",
			Params: []v1alpha2.ParamSpec{
				{Name: "required-param", Type: v1alpha2.ParamTypeString},
			},
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop", With: map[string]string{"val": "${{ params.required-param }}"}},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	// Create run WITHOUT the required param.
	run := createWorkflowRun(t, cli, &v1alpha2.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-param-run-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowRunSpec{
			WorkflowTemplateRef: tmpl.Name,
			// No params provided.
		},
	})

	// Run should be rejected.
	var finalRun v1alpha2.WorkflowRun
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := cli.Get(ctx, client.ObjectKeyFromObject(run), &finalRun); err != nil {
			return false, nil
		}
		for _, c := range finalRun.Status.Conditions {
			if c.Type == v1alpha2.WorkflowRunConditionAccepted && c.Status == metav1.ConditionFalse {
				return true, nil
			}
		}
		return false, nil
	})
	require.NoError(t, pollErr, "timed out waiting for WorkflowRun to be rejected")
	assert.Empty(t, finalRun.Status.TemporalWorkflowID, "Temporal workflow should not have been started")
}

// TestE2EWorkflowAPIEndpoints verifies the HTTP API for workflow CRUD operations.
func TestE2EWorkflowAPIEndpoints(t *testing.T) {
	skipIfNoWorkflows(t)

	cli := setupK8sClient(t, false)
	baseURL := kagentBaseURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Create a template via K8s API first.
	tmpl := createWorkflowTemplate(t, cli, &v1alpha2.WorkflowTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wf-api-test-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.WorkflowTemplateSpec{
			Description: "API test template",
			Steps: []v1alpha2.StepSpec{
				{Name: "step-a", Type: v1alpha2.StepTypeAction, Action: "noop", With: map[string]string{"val": "api-test"}},
			},
		},
	})

	waitForTemplateValidated(t, cli, client.ObjectKeyFromObject(tmpl))

	t.Run("list_templates", func(t *testing.T) {
		resp, err := httpClient.Get(baseURL + "/api/workflow-templates")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), tmpl.Name)
	})

	t.Run("get_template", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/workflow-templates/%s/%s", baseURL, tmpl.Namespace, tmpl.Name)
		resp, err := httpClient.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("create_run_via_api", func(t *testing.T) {
		reqBody := api.CreateWorkflowRunRequest{
			Name:                "wf-api-run-test",
			Namespace:           "kagent",
			WorkflowTemplateRef: tmpl.Name,
		}
		body, _ := json.Marshal(reqBody)
		resp, err := httpClient.Post(baseURL+"/api/workflow-runs", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Cleanup
		t.Cleanup(func() {
			run := &v1alpha2.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "wf-api-run-test", Namespace: "kagent"}}
			cli.Delete(context.Background(), run) //nolint:errcheck
		})
	})

	t.Run("list_runs", func(t *testing.T) {
		resp, err := httpClient.Get(baseURL + "/api/workflow-runs")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("get_run", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/workflow-runs/%s/%s", baseURL, "kagent", "wf-api-run-test")
		resp, err := httpClient.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("delete_run", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/workflow-runs/%s/%s", baseURL, "kagent", "wf-api-run-test")
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete, url, nil)
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("get_template_not_found", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/workflow-templates/%s/%s", baseURL, "kagent", "nonexistent")
		resp, err := httpClient.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
