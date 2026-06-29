package controller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/a2a"
)

func newTestScheduledRunScheduler(t *testing.T, kube client.Client) *ScheduledRunScheduler {
	t.Helper()
	scheduler, err := NewScheduledRunScheduler(kube, nil, a2a.NewAgentClientRegistry())
	require.NoError(t, err)
	return scheduler
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
		assert.Equal(t, "CRON_TZ=UTC 0 9 * * *", scheduleSpecForCron(sr))
	})
}

func TestScheduledRunScheduler_RemoveSchedule(t *testing.T) {
	scheduler := newTestScheduledRunScheduler(t, nil)

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

	s := newTestScheduledRunScheduler(t, kube)
	s.outcomePollerHook = nil // disable async outcome polling for deterministic asserts
	return s, types.NamespacedName{Namespace: sr.Namespace, Name: sr.Name}
}

func TestRouteKeyForScheduledRunTarget(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "agent"}

	got, err := routeKeyForScheduledRunTarget(v1alpha2.AgentReferenceKindAgent, key)
	require.NoError(t, err)
	assert.Equal(t, "default/agent", got)

	got, err = routeKeyForScheduledRunTarget(v1alpha2.AgentReferenceKindSandboxAgent, key)
	require.NoError(t, err)
	assert.Equal(t, "sandboxes/default/agent", got)

	_, err = routeKeyForScheduledRunTarget(v1alpha2.AgentReferenceKind("Other"), key)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported agentRef.kind")
}

func TestRunAgentCall_RequiresDatabaseClient(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "needs-db", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
	}
	s, _ := newSchedulerWithFake(t, sr)

	err := s.runAgentCall(context.Background(), sr, "session-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database client is not configured")
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
	assert.Equal(t, v1alpha2.RunStatusPending, entry.Status)
	assert.Nil(t, entry.EndTime)
	assert.True(t, called)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.RunStatusPending, got.Status.RunHistory[0].Status)
	assert.Nil(t, got.Status.RunHistory[0].EndTime)
	assert.Empty(t, got.Status.RunHistory[0].Message)
}

func TestRunOnce_SuspendedManualTriggerDoesNotDispatch(t *testing.T) {
	existingNextRunTime := metav1.NewTime(time.Now().Add(time.Hour))
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "suspended", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			Suspend:  true,
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
		Status: v1alpha2.ScheduledRunStatus{NextRunTime: &existingNextRunTime},
	}
	s, key := newSchedulerWithFake(t, sr)

	called := false
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) error {
		called = true
		return nil
	}

	entry, err := s.TriggerManualRun(key)
	require.Error(t, err)
	require.Nil(t, entry)
	assert.ErrorIs(t, err, errScheduledRunSuspended)
	assert.False(t, called)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Empty(t, got.Status.RunHistory)
	assert.Nil(t, got.Status.NextRunTime)
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
	assert.Equal(t, v1alpha2.RunStatusDispatchFailed, entry.Status)
	assert.NotNil(t, entry.EndTime)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.RunStatusDispatchFailed, got.Status.RunHistory[0].Status)
	assert.Contains(t, got.Status.RunHistory[0].Message, "agent down")
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
	assert.Equal(t, v1alpha2.RunStatusDispatchFailed, entry.Status)
	assert.Contains(t, entry.Message, "dispatch panic")

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RunHistory, 1)
	assert.Equal(t, v1alpha2.RunStatusDispatchFailed, got.Status.RunHistory[0].Status)
	assert.Contains(t, got.Status.RunHistory[0].Message, "dispatch panic")
}

func TestRunOnce_TrimsHistory(t *testing.T) {
	existing := make([]v1alpha2.RunHistoryEntry, 10)
	for i := range existing {
		existing[i] = v1alpha2.RunHistoryEntry{
			StartTime: metav1.NewTime(time.Now().Add(time.Duration(-i) * time.Minute)),
			Status:    v1alpha2.RunStatusPending,
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
	require.LessOrEqual(t, len(entry.Message), messageMaxBytes+len("…(truncated)"))
}

// --- poller path tests -----------------------------------------------------

// stubDBClient overrides only ListTasksForSession; any other method call
// panics, which is fine because the poller never touches them.
type stubDBClient struct {
	database.Client
	listFn func(ctx context.Context, sessionID string) ([]*a2atype.Task, error)
}

func (s *stubDBClient) ListTasksForSession(ctx context.Context, sessionID string) ([]*a2atype.Task, error) {
	return s.listFn(ctx, sessionID)
}

func shrinkPollCadence(t *testing.T, interval, timeout time.Duration) {
	t.Helper()
	prevInterval, prevTimeout := outcomePollInterval, outcomePollTimeout
	outcomePollInterval = interval
	outcomePollTimeout = timeout
	t.Cleanup(func() {
		outcomePollInterval = prevInterval
		outcomePollTimeout = prevTimeout
	})
}

func TestPollSessionOutcome(t *testing.T) {
	shrinkPollCadence(t, 5*time.Millisecond, 200*time.Millisecond)

	t.Run("completed maps to Succeeded", func(t *testing.T) {
		s := newTestScheduledRunScheduler(t, nil)
		s.dbClient = &stubDBClient{listFn: func(_ context.Context, _ string) ([]*a2atype.Task, error) {
			return []*a2atype.Task{{Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}}, nil
		}}
		status, _, err := s.pollSessionOutcome(context.Background(), "sess", "user")
		require.NoError(t, err)
		assert.Equal(t, v1alpha2.RunStatusSucceeded, status)
	})

	t.Run("failed maps to Failed with message", func(t *testing.T) {
		s := newTestScheduledRunScheduler(t, nil)
		s.dbClient = &stubDBClient{listFn: func(_ context.Context, _ string) ([]*a2atype.Task, error) {
			return []*a2atype.Task{{Status: a2atype.TaskStatus{
				State:   a2atype.TaskStateFailed,
				Message: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("boom")),
			}}}, nil
		}}
		status, msg, err := s.pollSessionOutcome(context.Background(), "sess", "user")
		require.NoError(t, err)
		assert.Equal(t, v1alpha2.RunStatusFailed, status)
		assert.Equal(t, "boom", msg)
	})

	t.Run("deadline maps to Timeout", func(t *testing.T) {
		s := newTestScheduledRunScheduler(t, nil)
		s.dbClient = &stubDBClient{listFn: func(_ context.Context, _ string) ([]*a2atype.Task, error) {
			return nil, nil
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		status, _, err := s.pollSessionOutcome(ctx, "sess", "user")
		require.NoError(t, err)
		assert.Equal(t, v1alpha2.RunStatusTimeout, status)
	})
}

// TestSpawnOutcomePoller_UpdatesMatchingEntry verifies the SessionID-keyed
// write: the poller must update the RunHistoryEntry whose SessionID matches,
// not by index — RunHistory can be trimmed between dispatch and resolution.
func TestSpawnOutcomePoller_UpdatesMatchingEntry(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	start := metav1.NewTime(time.Now())
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
		Status: v1alpha2.ScheduledRunStatus{
			RunHistory: []v1alpha2.RunHistoryEntry{
				{StartTime: start, SessionID: "other", Status: v1alpha2.RunStatusPending},
				{StartTime: start, SessionID: "target", Status: v1alpha2.RunStatusPending},
			},
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithRuntimeObjects(sr).
		Build()
	s := newTestScheduledRunScheduler(t, kube)
	s.outcomePollerHook = func(_ context.Context, _, _ string) (v1alpha2.RunStatus, string, error) {
		return v1alpha2.RunStatusSucceeded, "ok", nil
	}

	key := types.NamespacedName{Namespace: "default", Name: "sr"}
	s.spawnOutcomePoller(key, "target", "user")
	s.pollersWG.Wait()

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, kube.Get(context.Background(), key, got))
	assert.Equal(t, v1alpha2.RunStatusPending, got.Status.RunHistory[0].Status)
	assert.Equal(t, v1alpha2.RunStatusSucceeded, got.Status.RunHistory[1].Status)
	require.NotNil(t, got.Status.RunHistory[1].EndTime)
}

// TestResumePendingPollers verifies that on controller startup, Pending
// RunHistory entries spawn fresh outcome pollers, while terminal/empty-session
// entries are skipped.
func TestResumePendingPollers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	start := metav1.NewTime(time.Now())
	end := metav1.NewTime(time.Now())
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule: "0 * * * *",
			Prompt:   "hi",
			AgentRef: v1alpha2.AgentReference{Name: "a", Namespace: "default"},
		},
		Status: v1alpha2.ScheduledRunStatus{
			RunHistory: []v1alpha2.RunHistoryEntry{
				{StartTime: start, SessionID: "resume-me", Status: v1alpha2.RunStatusPending},
				{StartTime: start, SessionID: "done", Status: v1alpha2.RunStatusSucceeded, EndTime: &end},
				{StartTime: start, Status: v1alpha2.RunStatusPending}, // empty SessionID
			},
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithRuntimeObjects(sr).
		Build()
	s := newTestScheduledRunScheduler(t, kube)

	var (
		mu    sync.Mutex
		seen  []string
		ready = make(chan struct{})
	)
	s.outcomePollerHook = func(_ context.Context, sessionID, _ string) (v1alpha2.RunStatus, string, error) {
		mu.Lock()
		seen = append(seen, sessionID)
		mu.Unlock()
		close(ready)
		return v1alpha2.RunStatusSucceeded, "", nil
	}

	s.resumePendingPollers(context.Background())

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("expected resume not observed")
	}
	s.pollersWG.Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"resume-me"}, seen)
}
