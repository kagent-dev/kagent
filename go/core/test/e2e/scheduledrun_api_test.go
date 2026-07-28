package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
)

func scheduledRunTargetRef(name string) corev1.TypedLocalObjectReference {
	apiGroup := v1alpha2.ScheduledRunTargetAPIGroup
	return corev1.TypedLocalObjectReference{
		APIGroup: &apiGroup,
		Kind:     v1alpha2.ScheduledRunTargetKindAgent,
		Name:     name,
	}
}

func waitForAgentDeploymentStable(t *testing.T, cli client.Client, agent *v1alpha2.Agent) {
	t.Helper()
	require.Eventually(t, func() bool {
		deployment := &appsv1.Deployment{}
		if err := cli.Get(t.Context(), client.ObjectKeyFromObject(agent), deployment); err != nil {
			return false
		}
		return deployment.Status.ObservedGeneration >= deployment.Generation &&
			deployment.Status.UpdatedReplicas == 1 &&
			deployment.Status.AvailableReplicas == 1 &&
			deployment.Status.UnavailableReplicas == 0
	}, time.Minute, time.Second, "agent Deployment did not stabilize")
}

func waitForAgentInvocationReady(t *testing.T, agent *v1alpha2.Agent) {
	t.Helper()
	a2aClient := setupA2AClient(t, agent)
	require.Eventually(t, func() bool {
		message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?"))
		_, err := a2aClient.SendMessage(t.Context(), &a2atype.SendMessageRequest{Message: message})
		return err == nil
	}, 30*time.Second, time.Second, "agent A2A invocation endpoint did not become ready")
}

// TestScheduledRunAPI tests the ScheduledRun REST API lifecycle end-to-end.
// Requires a deployed kagent instance (set KAGENT_URL or default http://localhost:8083).
func TestE2EScheduledRunAPI(t *testing.T) {
	cli := setupK8sClient(t, false)
	baseURL := kagentURL() + "/api/scheduledruns"
	namespace := utils.GetResourceNamespace()

	mockURL, stopMock := setupMockServer(t, "mocks/invoke_golang_adk_agent.json")
	defer stopMock()
	modelConfig := setupModelConfig(t, cli, mockURL)
	goRuntime := v1alpha2.DeclarativeRuntime_Go
	agent := setupAgentWithOptions(t, cli, modelConfig.Name, nil, AgentOptions{
		SystemMessage: "Answer scheduled prompts.",
		Runtime:       &goRuntime,
	})
	require.Equal(t, namespace, agent.Namespace)
	waitForAgentDeploymentStable(t, cli, agent)
	waitForAgentInvocationReady(t, agent)

	srName := "e2e-test-sr-" + time.Now().Format("150405")

	t.Run("create", func(t *testing.T) {
		sr := v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      srName,
				Namespace: namespace,
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule:              "0 */2 * * *",
				TargetRef:             scheduledRunTargetRef(agent.Name),
				Prompt:                "What is 2+2?",
				RecentExecutionsLimit: new(int32(5)),
			},
		}
		body, _ := json.Marshal(sr)

		resp, respBody := doRequest(t, "POST", baseURL, body)
		assert.Equal(t, http.StatusCreated, resp.StatusCode, "body: %s", respBody)
	})

	t.Run("list", func(t *testing.T) {
		resp, body := doRequest(t, "GET", baseURL, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, string(body), srName)
	})

	t.Run("get", func(t *testing.T) {
		resp, body := doRequest(t, "GET", baseURL+"/"+namespace+"/"+srName, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result api.StandardResponse[v1alpha2.ScheduledRun]
		require.NoError(t, json.Unmarshal(body, &result))
		assert.Equal(t, "0 */2 * * *", result.Data.Spec.Schedule)
		assert.Equal(t, agent.Name, result.Data.Spec.TargetRef.Name)
		assert.Equal(t, "What is 2+2?", result.Data.Spec.Prompt)
		require.NotNil(t, result.Data.Spec.AllowSessionInteraction)
		assert.False(t, *result.Data.Spec.AllowSessionInteraction)
	})

	t.Run("update", func(t *testing.T) {
		allowSessionInteraction := true
		sr := v1alpha2.ScheduledRun{
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule:                "0 */3 * * *",
				TargetRef:               scheduledRunTargetRef(agent.Name),
				Prompt:                  "What is 2+2?",
				AllowSessionInteraction: &allowSessionInteraction,
				RecentExecutionsLimit:   new(int32(10)),
			},
		}
		body, _ := json.Marshal(sr)

		resp, respBody := doRequest(t, "PUT", baseURL+"/"+namespace+"/"+srName, body)
		assert.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", respBody)

		// Verify the update took effect
		var result api.StandardResponse[v1alpha2.ScheduledRun]
		_, getBody := doRequest(t, "GET", baseURL+"/"+namespace+"/"+srName, nil)
		require.NoError(t, json.Unmarshal(getBody, &result))
		assert.Equal(t, "0 */3 * * *", result.Data.Spec.Schedule)
		assert.Equal(t, "What is 2+2?", result.Data.Spec.Prompt)
		require.NotNil(t, result.Data.Spec.AllowSessionInteraction)
		assert.True(t, *result.Data.Spec.AllowSessionInteraction)
	})

	t.Run("create_invalid_schedule_defers_validation_to_controller", func(t *testing.T) {
		sr := v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-schedule-sr",
				Namespace: namespace,
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule:  "x x x x x", // valid shape, invalid cron fields
				TargetRef: scheduledRunTargetRef(agent.Name),
				Prompt:    "should fail",
			},
		}
		body, _ := json.Marshal(sr)

		resp, respBody := doRequest(t, "POST", baseURL, body)
		require.Equal(t, http.StatusCreated, resp.StatusCode, "body: %s", respBody)
		require.Eventually(t, func() bool {
			var current v1alpha2.ScheduledRun
			if err := cli.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: sr.Name}, &current); err != nil {
				return false
			}
			condition := meta.FindStatusCondition(current.Status.Conditions, v1alpha2.ScheduledRunConditionTypeAccepted)
			return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "InvalidSchedule"
		}, 30*time.Second, time.Second, "controller did not reject invalid cron syntax")
		defer func() {
			resp, _ := doRequest(t, "DELETE", baseURL+"/"+namespace+"/invalid-schedule-sr", nil)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		}()
	})

	t.Run("suspended_manual_trigger_allowed", func(t *testing.T) {
		sr := v1alpha2.ScheduledRun{
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule:              "0 */3 * * *",
				TargetRef:             scheduledRunTargetRef(agent.Name),
				Prompt:                "What is 2+2?",
				Suspended:             new(true),
				RecentExecutionsLimit: new(int32(10)),
			},
		}
		body, _ := json.Marshal(sr)

		resp, respBody := doRequest(t, "PUT", baseURL+"/"+namespace+"/"+srName, body)
		require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", respBody)

		resp, triggerBody := doRequest(t, "POST", baseURL+"/"+namespace+"/"+srName+"/trigger", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", triggerBody)
		var triggerResult api.StandardResponse[v1alpha2.ScheduledRunExecution]
		require.NoError(t, json.Unmarshal(triggerBody, &triggerResult))
		require.NotEqual(t, v1alpha2.ScheduledRunExecutionStatus_DispatchFailed, triggerResult.Data.Status, triggerResult.Data.StatusMessage)
		require.Equal(t, v1alpha2.ScheduledRunExecutionTrigger_Manual, triggerResult.Data.Trigger)

		var sessionID string
		require.Eventually(t, func() bool {
			current := &v1alpha2.ScheduledRun{}
			if err := cli.Get(t.Context(), client.ObjectKey{Namespace: namespace, Name: srName}, current); err != nil {
				return false
			}
			for _, entry := range current.Status.RecentExecutions {
				if entry.Status != v1alpha2.ScheduledRunExecutionStatus_Succeeded {
					continue
				}
				if entry.SessionID != nil {
					sessionID = *entry.SessionID
				}
				return sessionID != "" && entry.CompletionTime != nil
			}
			return false
		}, time.Minute, time.Second, "ScheduledRun did not reach Succeeded")

		resp, sessionBody := doRequest(t, "GET", kagentURL()+"/api/sessions/"+sessionID, nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", sessionBody)
	})

	t.Run("delete", func(t *testing.T) {
		resp, _ := doRequest(t, "DELETE", baseURL+"/"+namespace+"/"+srName, nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("get_after_delete", func(t *testing.T) {
		resp, _ := doRequest(t, "GET", baseURL+"/"+namespace+"/"+srName, nil)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// doRequest makes an HTTP request with optional JSON body and returns the response.
func doRequest(t *testing.T, method, url string, body []byte) (*http.Response, []byte) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()

	return resp, respBody
}
