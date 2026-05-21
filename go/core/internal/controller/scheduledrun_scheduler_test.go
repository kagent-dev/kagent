package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func TestScheduledRunScheduler_UpdateSchedule(t *testing.T) {
	scheduler := NewScheduledRunScheduler(nil, nil)

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
}

func TestScheduledRunScheduler_RemoveSchedule(t *testing.T) {
	scheduler := NewScheduledRunScheduler(nil, nil)

	t.Run("removes existing entry", func(t *testing.T) {
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

		err := scheduler.UpdateSchedule(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "to-remove", Namespace: "default"}
		_, exists := scheduler.entries[key]
		assert.True(t, exists)

		scheduler.RemoveSchedule(key)
		_, exists = scheduler.entries[key]
		assert.False(t, exists)
	})

	t.Run("no-op for non-existing entry", func(t *testing.T) {
		key := types.NamespacedName{Name: "nonexistent", Namespace: "default"}
		scheduler.RemoveSchedule(key)
	})
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

	s := NewScheduledRunScheduler(kube, nil)
	s.outcomePollerHook = nil // disable async outcome polling for deterministic asserts
	return s, types.NamespacedName{Namespace: sr.Namespace, Name: sr.Name}
}

func TestRunOnce_RecordsDispatched(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "ok", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
	}
	s, key := newSchedulerWithFake(t, sr)

	called := false
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) error {
		called = true
		return nil
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, v1alpha2.DispatchStatusDispatched, entry.DispatchStatus)
	assert.Equal(t, v1alpha2.RunOutcomePending, entry.Outcome)
	assert.True(t, called)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.DispatchStatusDispatched, got.Status.RunHistory[0].DispatchStatus)
	assert.Equal(t, v1alpha2.RunOutcomePending, got.Status.RunHistory[0].Outcome)
	assert.NotNil(t, got.Status.RunHistory[0].CompletionTime)
	assert.Empty(t, got.Status.RunHistory[0].DispatchMessage)
}

func TestRunOnce_RecordsDispatchFailed(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "boom", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
	}
	s, key := newSchedulerWithFake(t, sr)

	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) error {
		return errors.New("agent down")
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, v1alpha2.DispatchStatusFailed, entry.DispatchStatus)
	// Failed dispatches do not start a poller, so Outcome stays empty.
	assert.Empty(t, entry.Outcome)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.DispatchStatusFailed, got.Status.RunHistory[0].DispatchStatus)
	assert.Contains(t, got.Status.RunHistory[0].DispatchMessage, "agent down")
}

// TestRunOnce_RecoversFromDispatchPanic verifies that a panic inside the
// dispatch path is caught and recorded as a Failed entry instead of
// vanishing into the cron engine's recovery handler. Without this, the
// panic path silently drops the run from RunHistory.
func TestRunOnce_RecoversFromDispatchPanic(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "panicky", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
	}
	s, key := newSchedulerWithFake(t, sr)

	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) error {
		panic("simulated dispatch panic")
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, v1alpha2.DispatchStatusFailed, entry.DispatchStatus)
	assert.Contains(t, entry.DispatchMessage, "dispatch panic")

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.DispatchStatusFailed, got.Status.RunHistory[0].DispatchStatus)
	assert.Contains(t, got.Status.RunHistory[0].DispatchMessage, "dispatch panic")
}

func TestRunOnce_TrimsHistory(t *testing.T) {
	existing := make([]v1alpha2.RunHistoryEntry, 10)
	for i := range existing {
		existing[i] = v1alpha2.RunHistoryEntry{
			StartTime:      metav1.NewTime(time.Now().Add(time.Duration(-i) * time.Minute)),
			DispatchStatus: v1alpha2.DispatchStatusDispatched,
		}
	}
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "trim", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:      "0 * * * *",
			Prompt:        "hi",
			AgentRef:      v1alpha2.AgentReference{Name: "a", Namespace: "default"},
			MaxRunHistory: 5,
		},
		Status: v1alpha2.ScheduledRunStatus{RunHistory: existing},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) error { return nil }

	_, err := s.TriggerManualRun(key)
	require.NoError(t, err)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	assert.Len(t, got.Status.RunHistory, 5)
}

// TestRunOnce_TruncatesLongDispatchMessage guards the apiserver size budget:
// a flood of long error strings must not push status past the limit.
func TestRunOnce_TruncatesLongDispatchMessage(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "longmsg", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
	}
	s, key := newSchedulerWithFake(t, sr)

	long := make([]byte, messageMaxBytes*4)
	for i := range long {
		long[i] = 'x'
	}
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) error {
		return errors.New(string(long))
	}

	entry, err := s.TriggerManualRun(key)
	require.NoError(t, err)
	require.LessOrEqual(t, len(entry.DispatchMessage), messageMaxBytes+len("…(truncated)"))
}
