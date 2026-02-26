package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// cronJobAPIURL returns the base URL for the AgentCronJob API endpoints.
func cronJobAPIURL() string {
	kagentURL := os.Getenv("KAGENT_URL")
	if kagentURL == "" {
		kagentURL = "http://localhost:8083"
	}
	return kagentURL + "/api/cronjobs"
}

// cronJobResponse represents the API response for a single AgentCronJob.
type cronJobResponse struct {
	Error   bool                   `json:"error"`
	Data    *v1alpha2.AgentCronJob `json:"data,omitempty"`
	Message string                 `json:"message,omitempty"`
}

// cronJobListResponse represents the API response for a list of AgentCronJobs.
type cronJobListResponse struct {
	Error   bool                    `json:"error"`
	Data    []v1alpha2.AgentCronJob `json:"data,omitempty"`
	Message string                  `json:"message,omitempty"`
}

// TestE2EAgentCronJobCRUD tests the full CRUD lifecycle via HTTP API endpoints.
func TestE2EAgentCronJobCRUD(t *testing.T) {
	baseURL := cronJobAPIURL()
	httpClient := &http.Client{Timeout: 10 * time.Second}
	cli := setupK8sClient(t, false)

	cronJobName := fmt.Sprintf("e2e-cronjob-crud-%d", time.Now().Unix())
	namespace := "kagent"

	// Safety cleanup in case delete subtest doesn't run
	t.Cleanup(func() {
		cronJob := &v1alpha2.AgentCronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cronJobName,
				Namespace: namespace,
			},
		}
		_ = cli.Delete(context.Background(), cronJob)
	})

	// 1. Create
	t.Run("create", func(t *testing.T) {
		cronJob := v1alpha2.AgentCronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cronJobName,
				Namespace: namespace,
			},
			Spec: v1alpha2.AgentCronJobSpec{
				Schedule: "0 9 * * *",
				Prompt:   "Check cluster health",
				AgentRef: "non-existent-agent",
			},
		}

		body, err := json.Marshal(cronJob)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusCreated, resp.StatusCode)

		var result cronJobResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		require.False(t, result.Error)
		require.NotNil(t, result.Data)
		require.Equal(t, cronJobName, result.Data.Name)
		require.Equal(t, namespace, result.Data.Namespace)
	})

	// 2. Create duplicate → 409
	t.Run("create_duplicate", func(t *testing.T) {
		cronJob := v1alpha2.AgentCronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cronJobName,
				Namespace: namespace,
			},
			Spec: v1alpha2.AgentCronJobSpec{
				Schedule: "0 9 * * *",
				Prompt:   "Check cluster health",
				AgentRef: "non-existent-agent",
			},
		}

		body, err := json.Marshal(cronJob)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusConflict, resp.StatusCode)
	})

	// 3. Get
	t.Run("get", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s/%s", baseURL, namespace, cronJobName)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result cronJobResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		require.False(t, result.Error)
		require.NotNil(t, result.Data)
		require.Equal(t, cronJobName, result.Data.Name)
		require.Equal(t, "0 9 * * *", result.Data.Spec.Schedule)
		require.Equal(t, "Check cluster health", result.Data.Spec.Prompt)
		require.Equal(t, "non-existent-agent", result.Data.Spec.AgentRef)
	})

	// 4. List
	t.Run("list", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL, nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result cronJobListResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		require.False(t, result.Error)

		found := false
		for _, cj := range result.Data {
			if cj.Name == cronJobName {
				found = true
				break
			}
		}
		require.True(t, found, "Expected to find %s in list response", cronJobName)
	})

	// 5. Update
	t.Run("update", func(t *testing.T) {
		updatedCronJob := v1alpha2.AgentCronJob{
			Spec: v1alpha2.AgentCronJobSpec{
				Schedule: "0 10 * * *",
				Prompt:   "Updated cluster check",
				AgentRef: "non-existent-agent",
			},
		}

		body, err := json.Marshal(updatedCronJob)
		require.NoError(t, err)

		url := fmt.Sprintf("%s/%s/%s", baseURL, namespace, cronJobName)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)

		var result cronJobResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		require.False(t, result.Error)
		require.NotNil(t, result.Data)
		require.Equal(t, "0 10 * * *", result.Data.Spec.Schedule)
		require.Equal(t, "Updated cluster check", result.Data.Spec.Prompt)
	})

	// 6. Delete
	t.Run("delete", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s/%s", baseURL, namespace, cronJobName)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, url, nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 7. Get after delete → 404
	t.Run("get_after_delete", func(t *testing.T) {
		url := fmt.Sprintf("%s/%s/%s", baseURL, namespace, cronJobName)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// TestE2EAgentCronJobStatusAccepted verifies that a valid AgentCronJob
// gets Accepted=True condition and NextRunTime set by the controller.
func TestE2EAgentCronJobStatusAccepted(t *testing.T) {
	cli := setupK8sClient(t, false)

	cronJob := &v1alpha2.AgentCronJob{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-cronjob-status-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.AgentCronJobSpec{
			Schedule: "0 9 * * *",
			Prompt:   "Test prompt for status check",
			AgentRef: "some-agent",
		},
	}

	err := cli.Create(t.Context(), cronJob)
	require.NoError(t, err, "Failed to create AgentCronJob")
	cleanup(t, cli, cronJob)

	t.Logf("Created AgentCronJob %s/%s", cronJob.Namespace, cronJob.Name)

	// Poll until Accepted=True and NextRunTime is set
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		latest := &v1alpha2.AgentCronJob{}
		if err := cli.Get(ctx, client.ObjectKeyFromObject(cronJob), latest); err != nil {
			t.Logf("Failed to get AgentCronJob: %v", err)
			return false, nil
		}

		for _, cond := range latest.Status.Conditions {
			if cond.Type == v1alpha2.AgentCronJobConditionTypeAccepted && cond.Status == metav1.ConditionTrue {
				if latest.Status.NextRunTime != nil {
					return true, nil
				}
			}
		}
		return false, nil
	})
	require.NoError(t, pollErr, "Timed out waiting for Accepted=True and NextRunTime")

	// Verify final state
	final := &v1alpha2.AgentCronJob{}
	err = cli.Get(t.Context(), client.ObjectKeyFromObject(cronJob), final)
	require.NoError(t, err)

	require.NotNil(t, final.Status.NextRunTime, "NextRunTime should be set")
	t.Logf("Accepted=True, NextRunTime=%s", final.Status.NextRunTime.Time.Format(time.RFC3339))

	// Verify Ready condition is also set
	readyFound := false
	for _, cond := range final.Status.Conditions {
		if cond.Type == v1alpha2.AgentCronJobConditionTypeReady && cond.Status == metav1.ConditionTrue {
			readyFound = true
			break
		}
	}
	require.True(t, readyFound, "Expected Ready=True condition")
}

// TestE2EAgentCronJobInvalidSchedule verifies that an AgentCronJob with
// an invalid cron expression gets Accepted=False condition.
func TestE2EAgentCronJobInvalidSchedule(t *testing.T) {
	cli := setupK8sClient(t, false)

	cronJob := &v1alpha2.AgentCronJob{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-cronjob-invalid-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.AgentCronJobSpec{
			Schedule: "not-a-valid-cron",
			Prompt:   "Test prompt",
			AgentRef: "some-agent",
		},
	}

	err := cli.Create(t.Context(), cronJob)
	require.NoError(t, err, "Failed to create AgentCronJob")
	cleanup(t, cli, cronJob)

	t.Logf("Created AgentCronJob %s/%s with invalid schedule", cronJob.Namespace, cronJob.Name)

	// Poll until Accepted=False condition appears
	pollErr := wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		latest := &v1alpha2.AgentCronJob{}
		if err := cli.Get(ctx, client.ObjectKeyFromObject(cronJob), latest); err != nil {
			t.Logf("Failed to get AgentCronJob: %v", err)
			return false, nil
		}

		for _, cond := range latest.Status.Conditions {
			if cond.Type == v1alpha2.AgentCronJobConditionTypeAccepted && cond.Status == metav1.ConditionFalse {
				return true, nil
			}
		}
		return false, nil
	})
	require.NoError(t, pollErr, "Timed out waiting for Accepted=False condition")

	// Verify final state
	final := &v1alpha2.AgentCronJob{}
	err = cli.Get(t.Context(), client.ObjectKeyFromObject(cronJob), final)
	require.NoError(t, err)

	for _, cond := range final.Status.Conditions {
		if cond.Type == v1alpha2.AgentCronJobConditionTypeAccepted {
			require.Equal(t, metav1.ConditionFalse, cond.Status)
			require.Equal(t, "InvalidSchedule", cond.Reason)
			t.Logf("InvalidSchedule message: %s", cond.Message)
		}
	}

	require.Nil(t, final.Status.NextRunTime, "NextRunTime should not be set for invalid schedule")
}

// TestE2EAgentCronJobInvalidAgentRef verifies that when a cron job fires but
// the referenced agent doesn't exist, the status is set to Failed.
func TestE2EAgentCronJobInvalidAgentRef(t *testing.T) {
	cli := setupK8sClient(t, false)

	cronJob := &v1alpha2.AgentCronJob{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-cronjob-badref-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.AgentCronJobSpec{
			Schedule: "*/1 * * * *",
			Prompt:   "Test prompt with missing agent",
			AgentRef: "agent-that-does-not-exist",
		},
	}

	err := cli.Create(t.Context(), cronJob)
	require.NoError(t, err, "Failed to create AgentCronJob")
	cleanup(t, cli, cronJob)

	t.Logf("Created AgentCronJob %s/%s referencing non-existent agent", cronJob.Namespace, cronJob.Name)

	// Poll for LastRunResult=Failed. The cron fires every minute, so this may take up to ~60s.
	pollErr := wait.PollUntilContextTimeout(t.Context(), 5*time.Second, 120*time.Second, true, func(ctx context.Context) (bool, error) {
		latest := &v1alpha2.AgentCronJob{}
		if err := cli.Get(ctx, client.ObjectKeyFromObject(cronJob), latest); err != nil {
			t.Logf("Failed to get AgentCronJob: %v", err)
			return false, nil
		}

		if latest.Status.LastRunResult == "Failed" {
			return true, nil
		}

		t.Logf("Waiting for Failed status (current: %q, nextRun: %v)", latest.Status.LastRunResult, latest.Status.NextRunTime)
		return false, nil
	})
	require.NoError(t, pollErr, "Timed out waiting for LastRunResult=Failed")

	// Verify final state
	final := &v1alpha2.AgentCronJob{}
	err = cli.Get(t.Context(), client.ObjectKeyFromObject(cronJob), final)
	require.NoError(t, err)

	require.Equal(t, "Failed", final.Status.LastRunResult)
	require.NotEmpty(t, final.Status.LastRunMessage)
	require.Contains(t, final.Status.LastRunMessage, "not found")
	require.NotNil(t, final.Status.LastRunTime, "LastRunTime should be set after execution attempt")
	t.Logf("LastRunResult=Failed, message=%s", final.Status.LastRunMessage)
}
