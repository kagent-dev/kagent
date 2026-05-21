package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
)

// TestScheduledRunAPI tests the ScheduledRun REST API lifecycle end-to-end.
// Requires a deployed kagent instance (set KAGENT_URL or default http://localhost:8083).
func TestScheduledRunAPI(t *testing.T) {
	cli := setupK8sClient(t, false)
	baseURL := kagentURL() + "/api/scheduledruns"
	namespace := utils.GetResourceNamespace()

	// Create an Agent so the ScheduledRun controller can validate agentRef.
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-sr-agent-",
			Namespace:    namespace,
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				ModelConfig:   "default-model-config",
				SystemMessage: "test agent for ScheduledRun E2E",
			},
			Description: "agent for ScheduledRun E2E test",
		},
	}
	err := cli.Create(context.Background(), agent)
	require.NoError(t, err)
	cleanup(t, cli, agent)

	srName := "e2e-test-sr-" + time.Now().Format("150405")

	t.Run("create", func(t *testing.T) {
		sr := v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      srName,
				Namespace: namespace,
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				AgentRef: v1alpha2.AgentReference{
					Name:      agent.Name,
					Namespace: namespace,
				},
				Prompt:        "run e2e test task",
				MaxRunHistory: 5,
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
		assert.Equal(t, agent.Name, result.Data.Spec.AgentRef.Name)
		assert.Equal(t, "run e2e test task", result.Data.Spec.Prompt)
	})

	t.Run("update", func(t *testing.T) {
		sr := v1alpha2.ScheduledRun{
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */3 * * *",
				AgentRef: v1alpha2.AgentReference{
					Name:      agent.Name,
					Namespace: namespace,
				},
				Prompt:        "updated prompt",
				MaxRunHistory: 10,
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
		assert.Equal(t, "updated prompt", result.Data.Spec.Prompt)
	})

	t.Run("create_invalid_schedule", func(t *testing.T) {
		sr := v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-schedule-sr",
				Namespace: namespace,
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "not-a-valid-cron", // syntactically invalid
				AgentRef: v1alpha2.AgentReference{
					Name:      agent.Name,
					Namespace: namespace,
				},
				Prompt: "should fail",
			},
		}
		body, _ := json.Marshal(sr)

		resp, _ := doRequest(t, "POST", baseURL, body)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("trigger", func(t *testing.T) {
		resp, _ := doRequest(t, "POST", baseURL+"/"+namespace+"/"+srName+"/trigger", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
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
