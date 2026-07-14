package scheduledrun

import (
	"context"
	"errors"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
)

func newTestScheduledRunScheduler(t *testing.T, kube client.Client) *ScheduledRunScheduler {
	t.Helper()
	scheduler, err := NewScheduledRunScheduler(kube, nil, a2a.NewAgentClientRegistry())
	require.NoError(t, err)
	return scheduler
}

func testTargetRef(kind, name string) corev1.TypedObjectReference {
	apiGroup := v1alpha2.ScheduledRunTargetAPIGroup
	if kind == "" {
		kind = v1alpha2.ScheduledRunTargetKindAgent
	}
	return corev1.TypedObjectReference{
		APIGroup: &apiGroup,
		Kind:     kind,
		Name:     name,
	}
}

func submittedTaskResult() a2atype.SendMessageResult {
	return &a2atype.Task{
		ID:     a2atype.TaskID("task-id"),
		Status: a2atype.TaskStatus{State: a2atype.TaskStateSubmitted},
	}
}

func TestScheduledRunScheduler_UpdateSchedule(t *testing.T) {
	scheduler := newTestScheduledRunScheduler(t, nil)

	t.Run("adds entry for valid schedule", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				Prompt:   "hello",
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "test-sr", Namespace: "default"}
		_, exists := scheduler.entries[key]
		assert.True(t, exists, "entry should be registered")
	})

	t.Run("removes entry when suspended", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "suspended-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				Prompt:   "hello",
				Suspend:  false,
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "suspended-sr", Namespace: "default"}
		_, exists := scheduler.entries[key]
		assert.True(t, exists)

		sr.Spec.Suspend = true
		err = scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		_, exists = scheduler.entries[key]
		assert.False(t, exists, "entry should be removed when suspended")
	})

	t.Run("replaces entry on re-schedule", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "replace-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 */2 * * *",
				Prompt:   "hello",
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "replace-sr", Namespace: "default"}
		firstID := scheduler.entries[key]

		sr.Spec.Schedule = "0 */3 * * *"
		err = scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		secondID := scheduler.entries[key]
		assert.NotEqual(t, firstID, secondID, "entry ID should change on re-schedule")
	})

	t.Run("returns error for invalid cron expression", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "invalid",
				Prompt:   "hello",
			},
		}

		err := scheduler.UpdateSchedule(sr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add cron schedule")
	})

	t.Run("accepts schedule with TimeZone via CRON_TZ prefix", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tz-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 9 * * *",
				TimeZone: "America/Los_Angeles",
				Prompt:   "hello",
			},
		}
		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)
	})

	t.Run("defaults schedule time zone to UTC", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-tz-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 9 * * *",
				Prompt:   "hello",
			},
		}
		assert.Equal(t, "CRON_TZ=UTC 0 9 * * *", ScheduleSpecForCron(sr))
	})
}

func TestScheduledRunScheduler_RemoveSchedule(t *testing.T) {
	scheduler := newTestScheduledRunScheduler(t, nil)
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "to-remove",
			Namespace: "default",
		},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 */2 * * *",
			Prompt:   "hello",
		},
	}

	require.NoError(t, scheduler.UpdateSchedule(sr))

	key := types.NamespacedName{Name: "to-remove", Namespace: "default"}
	_, exists := scheduler.entries[key]
	require.True(t, exists)

	scheduler.RemoveSchedule(key)
	_, exists = scheduler.entries[key]
	assert.False(t, exists)
}

// --- runOnce tests ----------------------------------------------------------
//
// runOnce is the single code path for both cron ticks and manual triggers.
// We swap dispatchHook to avoid needing a real A2A server, and disable the
// async outcome poller so RunHistory entries stay deterministic.

func newSchedulerWithFake(t *testing.T, sr *v1alpha2.ScheduledRun) (*ScheduledRunScheduler, types.NamespacedName) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithRuntimeObjects(sr).
		Build()

	s := newTestScheduledRunScheduler(t, kube)
	s.outcomePollerHook = nil // disable async outcome polling for deterministic asserts
	return s, types.NamespacedName{Namespace: sr.Namespace, Name: sr.Name}
}

func TestRouteKeyForScheduledRunTarget(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "agent"}

	got, err := routeKeyForScheduledRunTarget(v1alpha2.ScheduledRunTargetKindAgent, key)
	require.NoError(t, err)
	assert.Equal(t, "default/agent", got)

	got, err = routeKeyForScheduledRunTarget(v1alpha2.ScheduledRunTargetKindSandboxAgent, key)
	require.NoError(t, err)
	assert.Equal(t, "sandboxes/default/agent", got)

	_, err = routeKeyForScheduledRunTarget("Other", key)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported targetRef.kind")
}

func TestRunAgentCall_RequiresDatabaseClient(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "needs-db", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, _ := newSchedulerWithFake(t, sr)

	_, err := s.runAgentCall(context.Background(), sr, "session-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database client is not configured")
}

func TestRunOnce_RecordsDispatched(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "ok", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, key := newSchedulerWithFake(t, sr)

	called := false
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		called = true
		return submittedTaskResult(), nil
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, v1alpha2.RunStatusInProgress, entry.Status)
	assert.Nil(t, entry.EndTime)
	assert.True(t, called)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.RunStatusInProgress, got.Status.RunHistory[0].Status)
	assert.Nil(t, got.Status.RunHistory[0].EndTime)
	assert.Empty(t, got.Status.RunHistory[0].Message)
}

func TestRunOnce_RecordsImmediateMessageSuccess(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "message", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, sessionID string) (a2atype.SendMessageResult, error) {
		message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("done"))
		message.ContextID = sessionID
		return message, nil
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	assert.Equal(t, v1alpha2.RunStatusSucceeded, entry.Status)
	assert.NotNil(t, entry.EndTime)
	assert.NotEmpty(t, entry.SessionID)
}

func TestRunOnce_RecordsImmediateTaskFailure(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-task", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		return &a2atype.Task{
			ID: a2atype.TaskID("failed-task-id"),
			Status: a2atype.TaskStatus{
				State:   a2atype.TaskStateFailed,
				Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("agent failed")),
			},
		}, nil
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	assert.Equal(t, v1alpha2.RunStatusFailed, entry.Status)
	assert.Equal(t, "agent failed", entry.Message)
	assert.NotNil(t, entry.EndTime)
}

func TestRunOnce_SuspendedManualTriggerDispatches(t *testing.T) {
	existingNextRunTime := metav1.NewTime(time.Now().Add(time.Hour))
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "suspended", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			Suspend:   true,
			TargetRef: testTargetRef("", "a"),
		},
		Status: v1alpha2.ScheduledRunStatus{NextRunTime: &existingNextRunTime},
	}
	s, key := newSchedulerWithFake(t, sr)

	called := false
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		called = true
		return submittedTaskResult(), nil
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.True(t, called)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Nil(t, got.Status.NextRunTime)
}

func TestRunOnce_RecordsDispatchFailed(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "boom", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, key := newSchedulerWithFake(t, sr)

	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		return nil, errors.New("agent down")
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, v1alpha2.RunStatusDispatchFailed, entry.Status)
	assert.NotNil(t, entry.EndTime)
	assert.Empty(t, entry.SessionID)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.RunStatusDispatchFailed, got.Status.RunHistory[0].Status)
	assert.Contains(t, got.Status.RunHistory[0].Message, "agent down")
}

func TestRunOnce_TrimsHistory(t *testing.T) {
	existing := make([]v1alpha2.RunHistoryEntry, 10)
	for i := range existing {
		existing[i] = v1alpha2.RunHistoryEntry{
			StartTime: metav1.NewTime(time.Now().Add(time.Duration(-i) * time.Minute)),
			Status:    v1alpha2.RunStatusInProgress,
		}
	}
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "trim", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:      "0 * * * *",
			Prompt:        "hi",
			TargetRef:     testTargetRef("", "a"),
			MaxRunHistory: 5,
		},
		Status: v1alpha2.ScheduledRunStatus{RunHistory: existing},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		return submittedTaskResult(), nil
	}

	_, err := s.TriggerManualRun(key)
	require.NoError(t, err)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	assert.Len(t, got.Status.RunHistory, 5)
}

// --- poller path tests -----------------------------------------------------

// TestSpawnOutcomePoller_UpdatesMatchingEntry verifies the SessionID-keyed
// write: the poller must update the RunHistoryEntry whose SessionID matches,
// not by index because RunHistory can be trimmed between dispatch and resolution.
func TestSpawnOutcomePoller_UpdatesMatchingEntry(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	start := metav1.NewTime(time.Now())
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
		Status: v1alpha2.ScheduledRunStatus{
			RunHistory: []v1alpha2.RunHistoryEntry{
				{StartTime: start, SessionID: "other", Status: v1alpha2.RunStatusInProgress},
				{StartTime: start, SessionID: "target", Status: v1alpha2.RunStatusInProgress},
			},
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithRuntimeObjects(sr).
		Build()
	s := newTestScheduledRunScheduler(t, kube)
	s.outcomePollerHook = func(_ context.Context, _, _ string) (v1alpha2.RunStatus, string) {
		return v1alpha2.RunStatusSucceeded, "ok"
	}

	key := types.NamespacedName{Namespace: "default", Name: "sr"}
	s.spawnOutcomePoller(key, "target", "default/a", "task-id")
	s.pollersWG.Wait()

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, kube.Get(context.Background(), key, got))
	assert.Equal(t, v1alpha2.RunStatusInProgress, got.Status.RunHistory[0].Status)
	assert.Equal(t, v1alpha2.RunStatusSucceeded, got.Status.RunHistory[1].Status)
	require.NotNil(t, got.Status.RunHistory[1].EndTime)
}
