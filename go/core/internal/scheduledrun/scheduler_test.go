package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	"sigs.k8s.io/controller-runtime/pkg/manager"

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

func testTargetRef(kind, name string) corev1.TypedLocalObjectReference {
	if kind == "" {
		kind = TargetKindAgent
	}
	apiGroup := TargetAPIGroup
	return corev1.TypedLocalObjectReference{APIGroup: &apiGroup, Kind: kind, Name: name}
}

func submittedTaskResult() a2atype.SendMessageResult {
	return &a2atype.Task{
		ID:     a2atype.TaskID("task-id"),
		Status: a2atype.TaskStatus{State: a2atype.TaskStateSubmitted},
	}
}

func TestExecutionStatusMessage(t *testing.T) {
	assert.Nil(t, executionStatusMessage(""))

	short := executionStatusMessage("target rejected the request")
	require.NotNil(t, short)
	assert.Equal(t, "target rejected the request", *short)

	long := executionStatusMessage(strings.Repeat("界", v1alpha2.MaxScheduledRunStatusMessageLength+1))
	require.NotNil(t, long)
	assert.Len(t, []rune(*long), v1alpha2.MaxScheduledRunStatusMessageLength)
}

type recordingDatabaseClient struct {
	database.Client
	executionRecords     []*database.ScheduledRunExecutionRecord
	inProgressExecutions []database.ScheduledRunExecutionRecord
	storeErr             error
}

func (c *recordingDatabaseClient) StoreScheduledRunExecution(_ context.Context, execution *database.ScheduledRunExecutionRecord) error {
	if c.storeErr != nil {
		return c.storeErr
	}
	c.executionRecords = append(c.executionRecords, execution)
	return nil
}

func (c *recordingDatabaseClient) ListInProgressScheduledRunExecutions(context.Context) ([]database.ScheduledRunExecutionRecord, error) {
	return c.inProgressExecutions, nil
}

func TestScheduledRunScheduler_UpdateCronEntry(t *testing.T) {
	scheduler := newTestScheduledRunScheduler(t, nil)

	t.Run("adds cron entry for valid schedule", func(t *testing.T) {
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

		err := scheduler.UpdateCronEntry(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "test-sr", Namespace: "default"}
		_, exists := scheduler.cronEntries[key]
		assert.True(t, exists, "cron entry should be registered")
	})

	t.Run("removes cron entry when suspended", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "suspended-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule:  "0 */2 * * *",
				Prompt:    "hello",
				Suspended: new(false),
			},
		}

		err := scheduler.UpdateCronEntry(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "suspended-sr", Namespace: "default"}
		_, exists := scheduler.cronEntries[key]
		assert.True(t, exists)

		sr.Spec.Suspended = new(true)
		err = scheduler.UpdateCronEntry(sr)
		require.NoError(t, err)

		_, exists = scheduler.cronEntries[key]
		assert.False(t, exists, "cron entry should be removed when suspended")
	})

	t.Run("replaces cron entry when schedule changes", func(t *testing.T) {
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

		err := scheduler.UpdateCronEntry(sr)
		require.NoError(t, err)

		key := types.NamespacedName{Name: "replace-sr", Namespace: "default"}
		firstID := scheduler.cronEntries[key]

		sr.Spec.Schedule = "0 */3 * * *"
		err = scheduler.UpdateCronEntry(sr)
		require.NoError(t, err)

		secondID := scheduler.cronEntries[key]
		assert.NotEqual(t, firstID, secondID, "cron entry ID should change when schedule changes")
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

		err := scheduler.UpdateCronEntry(sr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add cron entry")
	})

	t.Run("accepts schedule with TimeZone via CRON_TZ prefix", func(t *testing.T) {
		sr := &v1alpha2.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tz-sr",
				Namespace: "default",
			},
			Spec: v1alpha2.ScheduledRunSpec{
				Schedule: "0 9 * * *",
				TimeZone: new("America/Los_Angeles"),
				Prompt:   "hello",
			},
		}
		err := scheduler.UpdateCronEntry(sr)
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
		assert.Equal(t, "CRON_TZ=UTC 0 9 * * *", CronSpecForSchedule(sr))
	})
}

func TestScheduledRunScheduler_RemoveCronEntry(t *testing.T) {
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

	require.NoError(t, scheduler.UpdateCronEntry(sr))

	key := types.NamespacedName{Name: "to-remove", Namespace: "default"}
	_, exists := scheduler.cronEntries[key]
	require.True(t, exists)

	scheduler.RemoveCronEntry(key)
	_, exists = scheduler.cronEntries[key]
	assert.False(t, exists)
}

// --- executeOnce tests ------------------------------------------------------
//
// executeOnce is the single code path for both cron ticks and manual triggers.
// We swap dispatchHook to avoid needing a real A2A server, and disable the
// async outcome poller so RecentExecutions stay deterministic.

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
	s.active.Store(true)
	s.outcomePollerHook = nil // disable async outcome polling for deterministic asserts
	return s, types.NamespacedName{Namespace: sr.Namespace, Name: sr.Name}
}

func TestTriggerManualExecutionRunsOnFollowerAndLeaderRecoversPoller(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "manual-on-follower", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "agent"),
		},
	}
	follower, key := newSchedulerWithFake(t, sr)
	follower.active.Store(false)
	follower.dispatchHook = func(context.Context, *v1alpha2.ScheduledRun, string) (a2atype.SendMessageResult, error) {
		return submittedTaskResult(), nil
	}
	followerPollerCalled := make(chan struct{}, 1)
	follower.outcomePollerHook = func(context.Context, string, string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		followerPollerCalled <- struct{}{}
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "", nil
	}

	execution, err := follower.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, execution.Status)
	follower.pollersMu.Lock()
	followerPollerCount := len(follower.pollers)
	follower.pollersMu.Unlock()
	assert.Zero(t, followerPollerCount)
	select {
	case <-followerPollerCalled:
		t.Fatal("follower API replica started an outcome poller")
	default:
	}

	var inProgress v1alpha2.ScheduledRun
	require.NoError(t, follower.kube.Get(context.Background(), key, &inProgress))
	require.Len(t, inProgress.Status.RecentExecutions, 1)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, inProgress.Status.RecentExecutions[0].Status)

	leader := newTestScheduledRunScheduler(t, follower.kube)
	leader.active.Store(true)
	leader.outcomePollerHook = func(_ context.Context, routeKey, taskID string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		assert.Equal(t, "default/agent", routeKey)
		assert.Equal(t, "task-id", taskID)
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "", nil
	}
	leader.resumeOutcomePollersForScheduledRun(&inProgress)
	leader.pollersWG.Wait()

	var completed v1alpha2.ScheduledRun
	require.NoError(t, leader.kube.Get(context.Background(), key, &completed))
	require.Len(t, completed.Status.RecentExecutions, 1)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_Succeeded, completed.Status.RecentExecutions[0].Status)
}

func TestSchedulerStartActivatesAndDrains(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	s := newTestScheduledRunScheduler(t, kube)

	// The scheduler opts into leader election so the manager runs it only on the
	// elected leader, avoiding duplicate dispatch across replicas.
	leaderAware, isLeaderAware := any(s).(manager.LeaderElectionRunnable)
	require.True(t, isLeaderAware)
	assert.True(t, leaderAware.NeedLeaderElection())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Start(ctx)
	}()
	require.Eventually(t, s.active.Load, time.Second, time.Millisecond)

	cancel()
	require.NoError(t, <-done)
	assert.False(t, s.active.Load())
}

func TestRouteKeyForScheduledRunTarget(t *testing.T) {
	key := types.NamespacedName{Namespace: "default", Name: "agent"}

	got, err := routeKeyForScheduledRunTarget(TargetKindAgent, key)
	require.NoError(t, err)
	assert.Equal(t, "default/agent", got)

	got, err = routeKeyForScheduledRunTarget(TargetKindSandboxAgent, key)
	require.NoError(t, err)
	assert.Equal(t, "sandboxes/default/agent", got)

	_, err = routeKeyForScheduledRunTarget("Other", key)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported targetRef.kind")
}

func TestDispatchToTarget_RequiresDatabaseClient(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "needs-db", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, _ := newSchedulerWithFake(t, sr)

	_, err := s.dispatchToTarget(context.Background(), sr, "session-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database client is not configured")
}

func TestExecuteOnce_RecordsDispatched(t *testing.T) {
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

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, execution.Status)
	assert.Equal(t, new("task-id"), execution.TaskID)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionTrigger_Manual, execution.Trigger)
	assert.Nil(t, execution.CompletionTime)
	assert.True(t, called)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RecentExecutions, 1)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, got.Status.RecentExecutions[0].Status)
	assert.Equal(t, new("task-id"), got.Status.RecentExecutions[0].TaskID)
	assert.Nil(t, got.Status.RecentExecutions[0].CompletionTime)
	assert.Nil(t, got.Status.RecentExecutions[0].StatusMessage)
}

func TestExecuteOnce_PersistsDurableExecutionHistory(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "durable", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, key := newSchedulerWithFake(t, sr)
	db := &recordingDatabaseClient{}
	s.dbClient = db
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, sessionID string) (a2atype.SendMessageResult, error) {
		require.Len(t, db.executionRecords, 1, "ownership must be durable before dispatch")
		assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, db.executionRecords[0].Status)
		assert.Equal(t, new(sessionID), db.executionRecords[0].SessionID)
		assert.Nil(t, db.executionRecords[0].CompletionTime)
		message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("done"))
		message.ContextID = sessionID
		return message, nil
	}

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	require.Len(t, db.executionRecords, 2)
	finalRecord := db.executionRecords[1]
	assert.Equal(t, "default", finalRecord.ScheduledRunNamespace)
	assert.Equal(t, "durable", finalRecord.ScheduledRunName)
	assert.Equal(t, execution.Status, finalRecord.Status)
	assert.Equal(t, execution.SessionID, finalRecord.SessionID)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionTrigger_Manual, finalRecord.Trigger)
	assert.NotNil(t, finalRecord.CompletionTime)
}

func TestExecuteOnce_RecordsImmediateMessageSuccess(t *testing.T) {
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

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_Succeeded, execution.Status)
	assert.NotNil(t, execution.CompletionTime)
	assert.NotEmpty(t, execution.SessionID)
}

func TestExecuteOnce_RecordsScheduledTrigger(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "scheduled", Namespace: "default"},
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

	execution, err := s.executeOnce(context.Background(), key, false)
	require.NoError(t, err)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionTrigger_Scheduled, execution.Trigger)
}

func TestExecuteOnce_RecordsImmediateTaskFailure(t *testing.T) {
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

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_Failed, execution.Status)
	require.NotNil(t, execution.StatusMessage)
	assert.Equal(t, "agent failed", *execution.StatusMessage)
	assert.NotNil(t, execution.CompletionTime)
}

func TestExecuteOnce_SuspendedManualTriggerDispatches(t *testing.T) {
	existingNextExecutionTime := metav1.NewTime(time.Now().Add(time.Hour))
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "suspended", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			Suspended: new(true),
			TargetRef: testTargetRef("", "a"),
		},
		Status: v1alpha2.ScheduledRunStatus{NextExecutionTime: &existingNextExecutionTime},
	}
	s, key := newSchedulerWithFake(t, sr)

	called := false
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		called = true
		return submittedTaskResult(), nil
	}

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.True(t, called)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RecentExecutions, 1)
	assert.Nil(t, got.Status.NextExecutionTime)
}

func TestExecuteOnce_RecordsDispatchFailed(t *testing.T) {
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

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_DispatchFailed, execution.Status)
	assert.NotNil(t, execution.CompletionTime)
	assert.Empty(t, execution.SessionID)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	require.Len(t, got.Status.RecentExecutions, 1)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_DispatchFailed, got.Status.RecentExecutions[0].Status)
	require.NotNil(t, got.Status.RecentExecutions[0].StatusMessage)
	assert.Contains(t, *got.Status.RecentExecutions[0].StatusMessage, "agent down")
}

func TestExecuteOnce_KeepsPersistedSessionReachableOnDispatchError(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dispatch-error-with-session", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		return nil, &scheduledRunDispatchError{
			err:            errors.New("response lost after dispatch"),
			sessionCreated: true,
		}
	}

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_DispatchFailed, execution.Status)
	assert.NotEmpty(t, execution.SessionID)
}

func TestExecuteOnce_KeepsSessionReachableForInvalidDispatchResult(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid-dispatch-result", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:  "0 * * * *",
			Prompt:    "hi",
			TargetRef: testTargetRef("", "a"),
		},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		return nil, nil
	}

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_DispatchFailed, execution.Status)
	assert.NotEmpty(t, execution.SessionID)
}

func TestExecuteOnce_EnforcesExecutionTimeoutDuringDispatch(t *testing.T) {
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "dispatch-timeout", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:         "0 * * * *",
			Prompt:           "hi",
			TargetRef:        testTargetRef("", "a"),
			ExecutionTimeout: &metav1.Duration{Duration: 10 * time.Millisecond},
		},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(ctx context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	execution, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_TimedOut, execution.Status)
	require.NotNil(t, execution.StatusMessage)
	assert.Contains(t, *execution.StatusMessage, context.DeadlineExceeded.Error())
	assert.NotNil(t, execution.CompletionTime)
}

func TestExecuteOnce_TrimsExecutionHistory(t *testing.T) {
	existing := make([]v1alpha2.ScheduledRunExecution, 12)
	for i := range existing {
		status := v1alpha2.ScheduledRunExecutionStatus_Succeeded
		if i >= 7 {
			status = v1alpha2.ScheduledRunExecutionStatus_InProgress
		}
		existing[i] = v1alpha2.ScheduledRunExecution{
			ID:        fmt.Sprintf("execution-%d", i),
			StartTime: metav1.NewTime(time.Now().Add(time.Duration(-i) * time.Minute)),
			Status:    status,
		}
	}
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "trim", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			Schedule:              "0 * * * *",
			Prompt:                "hi",
			TargetRef:             testTargetRef("", "a"),
			RecentExecutionsLimit: new(int32(5)),
		},
		Status: v1alpha2.ScheduledRunStatus{RecentExecutions: existing},
	}
	s, key := newSchedulerWithFake(t, sr)
	s.dispatchHook = func(_ context.Context, _ *v1alpha2.ScheduledRun, _ string) (a2atype.SendMessageResult, error) {
		return submittedTaskResult(), nil
	}

	_, err := s.TriggerManualExecution(context.Background(), key)
	require.NoError(t, err)

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, s.kube.Get(context.Background(), key, got))
	assert.Len(t, got.Status.RecentExecutions, 11)
	assert.Len(t, filterRecentExecutions(got.Status.RecentExecutions, v1alpha2.ScheduledRunExecutionStatus_InProgress), 6)
	assert.Len(t, filterRecentExecutions(got.Status.RecentExecutions, v1alpha2.ScheduledRunExecutionStatus_Succeeded), 5)
}

func TestMergeExecutionStatusIsOrderIndependent(t *testing.T) {
	base := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	sr := &v1alpha2.ScheduledRun{
		Spec: v1alpha2.ScheduledRunSpec{RecentExecutionsLimit: new(int32(2))},
	}
	newest := v1alpha2.ScheduledRunExecution{
		ID: "newest", StartTime: metav1.NewTime(base.Add(2 * time.Minute)), Status: v1alpha2.ScheduledRunExecutionStatus_Succeeded,
	}
	oldest := v1alpha2.ScheduledRunExecution{
		ID: "oldest", StartTime: metav1.NewTime(base), Status: v1alpha2.ScheduledRunExecutionStatus_Succeeded,
	}
	middle := v1alpha2.ScheduledRunExecution{
		ID: "middle", StartTime: metav1.NewTime(base.Add(time.Minute)), Status: v1alpha2.ScheduledRunExecutionStatus_Succeeded,
	}

	// Simulate the newest execution writing first and an older, slower execution
	// completing last.
	mergeExecutionStatus(sr, newest)
	mergeExecutionStatus(sr, oldest)
	mergeExecutionStatus(sr, middle)

	require.NotNil(t, sr.Status.LastExecutionTime)
	assert.True(t, sr.Status.LastExecutionTime.Equal(&newest.StartTime))
	require.Len(t, sr.Status.RecentExecutions, 2)
	assert.Equal(t, "middle", sr.Status.RecentExecutions[0].ID)
	assert.Equal(t, "newest", sr.Status.RecentExecutions[1].ID)
}

func filterRecentExecutions(executions []v1alpha2.ScheduledRunExecution, status v1alpha2.ScheduledRunExecutionStatus) []v1alpha2.ScheduledRunExecution {
	var matching []v1alpha2.ScheduledRunExecution
	for _, execution := range executions {
		if execution.Status == status {
			matching = append(matching, execution)
		}
	}
	return matching
}

// --- poller path tests -----------------------------------------------------

// TestSpawnOutcomePoller_UpdatesMatchingExecution verifies the execution-ID-keyed
// write: the poller must update the ScheduledRunExecution whose ID matches,
// not by index because RecentExecutions can be trimmed between dispatch and resolution.
func TestSpawnOutcomePoller_UpdatesMatchingExecution(t *testing.T) {
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
			RecentExecutions: []v1alpha2.ScheduledRunExecution{
				{ID: "other-execution", StartTime: start, SessionID: new("other"), Status: v1alpha2.ScheduledRunExecutionStatus_InProgress},
				{ID: "target-execution", StartTime: start, SessionID: new("target"), TaskID: new("task-id"), Status: v1alpha2.ScheduledRunExecutionStatus_InProgress},
			},
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithRuntimeObjects(sr).
		Build()
	s := newTestScheduledRunScheduler(t, kube)
	s.outcomePollerHook = func(_ context.Context, _, _ string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "ok", nil
	}

	key := types.NamespacedName{Namespace: "default", Name: "sr"}
	s.spawnOutcomePoller(key, "", v1alpha2.ScheduledRunExecution{ID: "target-execution", SessionID: new("target"), TaskID: new("task-id")}, "default/a", time.Minute)
	s.pollersWG.Wait()

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, kube.Get(context.Background(), key, got))
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, got.Status.RecentExecutions[0].Status)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_Succeeded, got.Status.RecentExecutions[1].Status)
	assert.Equal(t, new("task-id"), got.Status.RecentExecutions[1].TaskID)
	require.NotNil(t, got.Status.RecentExecutions[1].CompletionTime)
}

func TestResumeInProgressPollers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			TargetRef: testTargetRef(TargetKindAgent, "agent"),
		},
		Status: v1alpha2.ScheduledRunStatus{
			RecentExecutions: []v1alpha2.ScheduledRunExecution{{
				ID:        "execution-id",
				SessionID: new("session-id"),
				TaskID:    new("task-id"),
				Status:    v1alpha2.ScheduledRunExecutionStatus_InProgress,
			}},
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithObjects(sr).
		Build()
	db := &recordingDatabaseClient{}
	s := newTestScheduledRunScheduler(t, kube)
	s.dbClient = db
	s.outcomePollerHook = func(_ context.Context, routeKey, taskID string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		assert.Equal(t, "default/agent", routeKey)
		assert.Equal(t, "task-id", taskID)
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "", nil
	}

	require.NoError(t, s.resumeInProgressPollers(context.Background()))
	s.pollersWG.Wait()

	got := &v1alpha2.ScheduledRun{}
	require.NoError(t, kube.Get(context.Background(), client.ObjectKeyFromObject(sr), got))
	require.Len(t, got.Status.RecentExecutions, 1)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_Succeeded, got.Status.RecentExecutions[0].Status)
	assert.Equal(t, new("task-id"), got.Status.RecentExecutions[0].TaskID)
	require.Len(t, db.executionRecords, 1)
	assert.Equal(t, "execution-id", db.executionRecords[0].ID)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_Succeeded, db.executionRecords[0].Status)
}

func TestResumeInProgressPollers_RecoversFromDurableExecutionHistory(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	uid := types.UID("scheduled-run-uid")
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default", UID: uid},
		Spec: v1alpha2.ScheduledRunSpec{
			TargetRef: testTargetRef(TargetKindAgent, "agent"),
		},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithObjects(sr).
		Build()
	sessionID := "session-id"
	taskID := "task-id"
	db := &recordingDatabaseClient{inProgressExecutions: []database.ScheduledRunExecutionRecord{{
		ID:                    "execution-id",
		ScheduledRunNamespace: sr.Namespace,
		ScheduledRunName:      sr.Name,
		ScheduledRunUID:       string(uid),
		StartTime:             time.Now(),
		Trigger:               v1alpha2.ScheduledRunExecutionTrigger_Scheduled,
		SessionID:             &sessionID,
		TaskID:                &taskID,
		Status:                v1alpha2.ScheduledRunExecutionStatus_InProgress,
	}}}
	s := newTestScheduledRunScheduler(t, kube)
	s.dbClient = db
	s.outcomePollerHook = func(_ context.Context, routeKey, gotTaskID string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		assert.Equal(t, "default/agent", routeKey)
		assert.Equal(t, taskID, gotTaskID)
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "", nil
	}

	require.NoError(t, s.resumeInProgressPollers(context.Background()))
	s.pollersWG.Wait()

	var got v1alpha2.ScheduledRun
	require.NoError(t, kube.Get(context.Background(), client.ObjectKeyFromObject(sr), &got))
	require.Len(t, got.Status.RecentExecutions, 1)
	assert.Equal(t, "execution-id", got.Status.RecentExecutions[0].ID)
	assert.Equal(t, &sessionID, got.Status.RecentExecutions[0].SessionID)
	assert.Equal(t, &taskID, got.Status.RecentExecutions[0].TaskID)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_Succeeded, got.Status.RecentExecutions[0].Status)
}

func TestSpawnOutcomePoller_ManagerCancellationLeavesExecutionRecoverable(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	start := metav1.Now()
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default"},
		Status: v1alpha2.ScheduledRunStatus{RecentExecutions: []v1alpha2.ScheduledRunExecution{{
			ID:        "execution-id",
			StartTime: start,
			SessionID: new("session-id"),
			TaskID:    new("task-id"),
			Status:    v1alpha2.ScheduledRunExecutionStatus_InProgress,
		}}},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithObjects(sr).
		Build()
	s := newTestScheduledRunScheduler(t, kube)
	managerCtx, cancelManager := context.WithCancel(context.Background())
	s.managerCtx.Store(&managerCtx)
	started := make(chan struct{})
	s.outcomePollerHook = func(ctx context.Context, _, _ string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		close(started)
		<-ctx.Done()
		return "", "", ctx.Err()
	}

	key := client.ObjectKeyFromObject(sr)
	s.spawnOutcomePoller(key, "", sr.Status.RecentExecutions[0], "default/agent", time.Minute)
	<-started
	cancelManager()
	s.pollersWG.Wait()

	var got v1alpha2.ScheduledRun
	require.NoError(t, kube.Get(context.Background(), key, &got))
	require.Len(t, got.Status.RecentExecutions, 1)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, got.Status.RecentExecutions[0].Status)
	assert.Equal(t, new("task-id"), got.Status.RecentExecutions[0].TaskID)
	assert.Nil(t, got.Status.RecentExecutions[0].CompletionTime)
}

func TestSpawnOutcomePoller_DurableWriteFailureLeavesExecutionRecoverable(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	start := metav1.Now()
	execution := v1alpha2.ScheduledRunExecution{
		ID:        "execution-id",
		StartTime: start,
		SessionID: new("session-id"),
		TaskID:    new("task-id"),
		Status:    v1alpha2.ScheduledRunExecutionStatus_InProgress,
	}
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default"},
		Status:     v1alpha2.ScheduledRunStatus{RecentExecutions: []v1alpha2.ScheduledRunExecution{execution}},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithObjects(sr).
		Build()
	s := newTestScheduledRunScheduler(t, kube)
	s.dbClient = &recordingDatabaseClient{storeErr: errors.New("database unavailable")}
	s.outcomePollerHook = func(context.Context, string, string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "", nil
	}

	key := client.ObjectKeyFromObject(sr)
	s.spawnOutcomePoller(key, "", execution, "default/agent", time.Minute)
	s.pollersWG.Wait()

	var got v1alpha2.ScheduledRun
	require.NoError(t, kube.Get(context.Background(), key, &got))
	require.Len(t, got.Status.RecentExecutions, 1)
	assert.Equal(t, v1alpha2.ScheduledRunExecutionStatus_InProgress, got.Status.RecentExecutions[0].Status)
	assert.Equal(t, new("task-id"), got.Status.RecentExecutions[0].TaskID)
}

func TestResumeInProgressPollers_DeduplicatesActiveTask(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	sr := &v1alpha2.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Name: "sr", Namespace: "default"},
		Spec: v1alpha2.ScheduledRunSpec{
			TargetRef: testTargetRef(TargetKindAgent, "agent"),
		},
		Status: v1alpha2.ScheduledRunStatus{RecentExecutions: []v1alpha2.ScheduledRunExecution{{
			ID:        "execution-id",
			SessionID: new("session-id"),
			TaskID:    new("task-id"),
			Status:    v1alpha2.ScheduledRunExecutionStatus_InProgress,
		}}},
	}
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha2.ScheduledRun{}).
		WithObjects(sr).
		Build()
	s := newTestScheduledRunScheduler(t, kube)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	s.outcomePollerHook = func(context.Context, string, string) (v1alpha2.ScheduledRunExecutionStatus, string, error) {
		started <- struct{}{}
		<-release
		return v1alpha2.ScheduledRunExecutionStatus_Succeeded, "", nil
	}

	s.resumeOutcomePollersForScheduledRun(sr)
	s.resumeOutcomePollersForScheduledRun(sr)
	<-started
	select {
	case <-started:
		t.Fatal("duplicate outcome poller started for the same task")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	s.pollersWG.Wait()
}
