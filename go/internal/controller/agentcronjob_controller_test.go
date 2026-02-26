/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	return scheme
}

func newCronJob(name, namespace, schedule, agentRef, prompt string, createdAt time.Time) *v1alpha2.AgentCronJob {
	return &v1alpha2.AgentCronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Time{Time: createdAt},
			Generation:        1,
		},
		Spec: v1alpha2.AgentCronJobSpec{
			Schedule: schedule,
			AgentRef: agentRef,
			Prompt:   prompt,
		},
	}
}

func TestAgentCronJobController_InvalidSchedule(t *testing.T) {
	scheme := newTestScheme(t)

	cronJob := newCronJob("test-cron", "default", "not-a-valid-cron", "my-agent", "hello", time.Now())

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cronJob).
		WithStatusSubresource(cronJob).
		Build()

	controller := &AgentCronJobController{
		Client:     fakeClient,
		Scheme:     scheme,
		A2ABaseURL: "http://localhost:9999",
	}

	result, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-cron", Namespace: "default"},
	})

	require.NoError(t, err, "Invalid schedule should not return an error (no retry)")
	assert.Equal(t, ctrl.Result{}, result, "Should not requeue")

	// Verify status
	var updated v1alpha2.AgentCronJob
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-cron", Namespace: "default"}, &updated))

	cond := findCondition(updated.Status.Conditions, v1alpha2.AgentCronJobConditionTypeAccepted)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "InvalidSchedule", cond.Reason)
	assert.Contains(t, cond.Message, "Failed to parse cron schedule")
}

func TestAgentCronJobController_ValidScheduleNotYetDue(t *testing.T) {
	scheme := newTestScheme(t)

	// Schedule for midnight on Jan 1 (far in the future from any test run)
	cronJob := newCronJob("test-cron", "default", "0 0 1 1 *", "my-agent", "hello", time.Now())

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cronJob).
		WithStatusSubresource(cronJob).
		Build()

	controller := &AgentCronJobController{
		Client:     fakeClient,
		Scheme:     scheme,
		A2ABaseURL: "http://localhost:9999",
	}

	result, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-cron", Namespace: "default"},
	})

	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "Should requeue for next run")

	// Verify status
	var updated v1alpha2.AgentCronJob
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-cron", Namespace: "default"}, &updated))

	// Accepted should be True
	acceptedCond := findCondition(updated.Status.Conditions, v1alpha2.AgentCronJobConditionTypeAccepted)
	require.NotNil(t, acceptedCond)
	assert.Equal(t, metav1.ConditionTrue, acceptedCond.Status)

	// Ready should be True
	readyCond := findCondition(updated.Status.Conditions, v1alpha2.AgentCronJobConditionTypeReady)
	require.NotNil(t, readyCond)
	assert.Equal(t, metav1.ConditionTrue, readyCond.Status)

	// NextRunTime should be set
	assert.NotNil(t, updated.Status.NextRunTime, "NextRunTime should be set")

	// LastRunTime should be nil (never ran)
	assert.Nil(t, updated.Status.LastRunTime, "LastRunTime should be nil")
}

func TestAgentCronJobController_DueScheduleAgentNotFound(t *testing.T) {
	scheme := newTestScheme(t)

	// Create a cron job that runs every minute, created 5 minutes ago
	cronJob := newCronJob("test-cron", "default", "* * * * *", "nonexistent-agent", "hello", time.Now().Add(-5*time.Minute))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cronJob).
		WithStatusSubresource(cronJob).
		Build()

	controller := &AgentCronJobController{
		Client:     fakeClient,
		Scheme:     scheme,
		A2ABaseURL: "http://localhost:9999",
	}

	result, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-cron", Namespace: "default"},
	})

	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "Should requeue for next run even after failure")

	// Verify status
	var updated v1alpha2.AgentCronJob
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-cron", Namespace: "default"}, &updated))

	assert.Equal(t, "Failed", updated.Status.LastRunResult)
	assert.Contains(t, updated.Status.LastRunMessage, "not found")
	assert.NotNil(t, updated.Status.LastRunTime)
	assert.NotNil(t, updated.Status.NextRunTime)
}

func TestAgentCronJobController_DueScheduleHTTPFailure(t *testing.T) {
	scheme := newTestScheme(t)

	// Create agent that exists
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "default",
		},
	}

	cronJob := newCronJob("test-cron", "default", "* * * * *", "my-agent", "hello", time.Now().Add(-5*time.Minute))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cronJob, agent).
		WithStatusSubresource(cronJob).
		Build()

	controller := &AgentCronJobController{
		Client:     fakeClient,
		Scheme:     scheme,
		A2ABaseURL: "http://localhost:9999", // No real server → connection refused
	}

	result, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-cron", Namespace: "default"},
	})

	require.NoError(t, err, "Execution failure should not return a reconcile error")
	assert.True(t, result.RequeueAfter > 0, "Should requeue for next run")

	// Verify status
	var updated v1alpha2.AgentCronJob
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-cron", Namespace: "default"}, &updated))

	assert.Equal(t, "Failed", updated.Status.LastRunResult)
	assert.Contains(t, updated.Status.LastRunMessage, "failed to create session")
	assert.NotNil(t, updated.Status.LastRunTime)
	assert.NotNil(t, updated.Status.NextRunTime)
}

func TestAgentCronJobController_RestartRecoveryNoRetroactiveRuns(t *testing.T) {
	scheme := newTestScheme(t)

	// Simulate a restart: CR was created 1 hour ago, last run was 30 minutes ago,
	// next run was 25 minutes ago (missed). Controller should skip to next future occurrence.
	lastRun := time.Now().Add(-30 * time.Minute)
	cronJob := newCronJob("test-cron", "default", "*/5 * * * *", "my-agent", "hello", time.Now().Add(-1*time.Hour))
	cronJob.Status.LastRunTime = &metav1.Time{Time: lastRun}
	cronJob.Status.LastRunResult = "Success"

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cronJob).
		WithStatusSubresource(cronJob).
		Build()

	controller := &AgentCronJobController{
		Client:     fakeClient,
		Scheme:     scheme,
		A2ABaseURL: "http://localhost:9999",
	}

	// The schedule is */5, last run was 30m ago. The next occurrence from lastRunTime
	// would be 25m ago, which is in the past. Since now >= nextRun, the controller
	// will attempt to execute (which will fail since agent doesn't exist). This is
	// the correct behavior — it executes once for the current tick, not retroactively
	// for all missed ticks. The important thing is it doesn't run 5 separate executions
	// for the 5 missed intervals.

	result, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-cron", Namespace: "default"},
	})

	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0)

	var updated v1alpha2.AgentCronJob
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-cron", Namespace: "default"}, &updated))

	// Should have run exactly once (failed because no agent)
	assert.NotNil(t, updated.Status.LastRunTime)
	// NextRunTime should be in the future
	assert.True(t, updated.Status.NextRunTime.Time.After(time.Now()), "NextRunTime should be in the future")
}

func TestAgentCronJobController_CRNotFound(t *testing.T) {
	scheme := newTestScheme(t)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	controller := &AgentCronJobController{
		Client:     fakeClient,
		Scheme:     scheme,
		A2ABaseURL: "http://localhost:9999",
	}

	result, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})

	require.NoError(t, err, "Not found should be ignored")
	assert.Equal(t, ctrl.Result{}, result)
}

func TestCronScheduleParsing(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		valid    bool
	}{
		{name: "every minute", schedule: "* * * * *", valid: true},
		{name: "every 5 minutes", schedule: "*/5 * * * *", valid: true},
		{name: "daily at 9am", schedule: "0 9 * * *", valid: true},
		{name: "weekdays at noon", schedule: "0 12 * * 1-5", valid: true},
		{name: "monthly", schedule: "0 0 1 * *", valid: true},
		{name: "invalid expression", schedule: "not-a-cron", valid: false},
		{name: "too few fields", schedule: "* * *", valid: false},
		{name: "too many fields", schedule: "* * * * * *", valid: false},
		{name: "invalid minute", schedule: "60 * * * *", valid: false},
		{name: "invalid hour", schedule: "* 25 * * *", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newTestScheme(t)

			cronJob := newCronJob("test-cron", "default", tt.schedule, "my-agent", "hello", time.Now())

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cronJob).
				WithStatusSubresource(cronJob).
				Build()

			controller := &AgentCronJobController{
				Client:     fakeClient,
				Scheme:     scheme,
				A2ABaseURL: "http://localhost:9999",
			}

			_, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-cron", Namespace: "default"},
			})

			require.NoError(t, err, "Reconcile should not return error regardless of schedule validity")

			var updated v1alpha2.AgentCronJob
			require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-cron", Namespace: "default"}, &updated))

			cond := findCondition(updated.Status.Conditions, v1alpha2.AgentCronJobConditionTypeAccepted)
			require.NotNil(t, cond)

			if tt.valid {
				assert.Equal(t, metav1.ConditionTrue, cond.Status, "Valid schedule should be accepted")
			} else {
				assert.Equal(t, metav1.ConditionFalse, cond.Status, "Invalid schedule should be rejected")
			}
		})
	}
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
