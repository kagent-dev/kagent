package agentinstancetask

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
)

type subscriptionStore struct {
	*testStore
	records   []database.TaskEvent
	afterRead func()
	err       error
	reads     int
}

func (s *subscriptionStore) ListAgentInstanceTaskEvents(_ context.Context, _, _ string, after int64, limit int) ([]database.TaskEvent, error) {
	s.reads++
	var page []database.TaskEvent
	for _, record := range s.records {
		if record.Sequence > after && len(page) < limit {
			page = append(page, record)
		}
	}
	if s.afterRead != nil {
		s.afterRead()
	}
	return page, s.err
}

type testUpdates struct {
	changed chan struct{}
	err     error
}

func (u *testUpdates) Changes() (<-chan struct{}, error) { return u.changed, u.err }

func subscriptionFixture(t *testing.T) (context.Context, *subscriptionStore, *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	id := uuid.NewString()
	task := &a2a.Task{ID: "task", ContextID: id, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	task.History = []*a2a.Message{a2a.NewMessageForTask(a2a.MessageRoleUser, task, a2a.NewTextPart("input"))}
	store := &subscriptionStore{testStore: &testStore{instance: &apiv1alpha1.AgentInstance{Id: id, Creator: "alice", ContextId: id}, tasks: []*a2a.Task{task}}}
	return auth.AuthSessionTo(ctx, testSession("alice")), store, NewService(store, &testAuthorizer{})
}

func TestSubscriptionDrainsFinalHistoryAcrossPages(t *testing.T) {
	ctx, store, service := subscriptionFixture(t)
	task := store.tasks[0]
	task.Artifacts = []*a2a.Artifact{{ID: "answer", Parts: a2a.ContentParts{a2a.NewTextPart("first")}}}
	// The terminal status occupies the last row of a page, before its archive.
	for i := range 127 {
		store.records = append(store.records, database.TaskEvent{Sequence: int64(i + 1), Event: &a2a.TaskArtifactUpdateEvent{
			TaskID: task.ID, ContextID: task.ContextID, Append: true, LastChunk: i == 126,
			Artifact: &a2a.Artifact{ID: "answer", Parts: a2a.ContentParts{a2a.NewTextPart("more")}},
		}})
	}
	output := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("finished"))
	store.records = append(store.records,
		database.TaskEvent{Sequence: 128, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)},
		database.TaskEvent{Sequence: 129, Event: output},
	)
	var got *a2a.Task
	appends := 0
	for event, err := range service.SubscribeToTask(ctx, store.instance.Id, &a2a.SubscribeToTaskRequest{ID: task.ID}, nil) {
		require.NoError(t, err)
		if update, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
			appends++
			require.Equal(t, appends == 127, update.LastChunk)
		}
		if got == nil {
			got = event.(*a2a.Task)
		} else {
			got, err = a2aevent.ApplyUpdate(got, event)
			require.NoError(t, err)
		}
		if got.Status.State.Terminal() {
			require.Equal(t, []*a2a.Message{task.History[0], output}, got.History, "the first completed frame must include its output")
		}
	}
	require.Equal(t, a2a.TaskStateCompleted, got.Status.State)
	require.Len(t, got.Artifacts[0].Parts, 128)
	require.Equal(t, 127, appends)
	require.Equal(t, 2, store.reads)
	require.Len(t, task.Artifacts[0].Parts, 1, "observation must not mutate its snapshot")
}

func TestSubscriptionSeesCommitBetweenReadAndWait(t *testing.T) {
	ctx, store, service := subscriptionFixture(t)
	task := store.tasks[0]
	updates := &testUpdates{changed: make(chan struct{})}
	store.afterRead = func() {
		store.afterRead = nil
		store.records = []database.TaskEvent{{Sequence: 1, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateInputRequired, nil)}}
		close(updates.changed)
		updates.changed = nil
	}
	var final *a2a.Task
	for event, err := range service.SubscribeToTask(ctx, store.instance.Id, &a2a.SubscribeToTaskRequest{ID: task.ID}, updates) {
		require.NoError(t, err)
		final = event.(*a2a.Task)
	}
	require.Equal(t, a2a.TaskStateInputRequired, final.Status.State)
	require.Equal(t, 2, store.reads)
}

func TestSubscriptionKeepsArchiveRowsOutOfLiveUpdates(t *testing.T) {
	ctx, store, service := subscriptionFixture(t)
	task := store.tasks[0]
	first := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("first"))
	second := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("second"))
	store.records = []database.TaskEvent{
		{Sequence: 1, Event: first},
		{Sequence: 2, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, second)},
		{Sequence: 3, Event: second},
		{Sequence: 4, Event: a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)},
	}
	var frames []a2a.Event
	for event, err := range service.SubscribeToTask(ctx, store.instance.Id, &a2a.SubscribeToTaskRequest{ID: task.ID}, nil) {
		require.NoError(t, err)
		frames = append(frames, event)
	}
	require.Len(t, frames, 3, "only the initial snapshot, live update, and final snapshot are public")
	require.IsType(t, &a2a.TaskStatusUpdateEvent{}, frames[1])
	require.Equal(t, []*a2a.Message{task.History[0], first, second}, frames[2].(*a2a.Task).History)
}

func TestSubscriptionReportsLostIngestionWithoutFinishingTask(t *testing.T) {
	for _, test := range []struct {
		name                        string
		readErr, ingestionErr, want error
	}{
		{name: "missing owner", want: a2a.ErrInternalError},
		{name: "failed owner", ingestionErr: a2a.ErrUnsupportedOperation, want: a2a.ErrUnsupportedOperation},
		{name: "deleted task", readErr: database.ErrNotFound, want: a2a.ErrTaskNotFound},
		{name: "failed read", readErr: errors.New("database failure"), want: a2a.ErrInternalError},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service := subscriptionFixture(t)
			store.err = test.readErr
			var lastErr error
			frames := 0
			for event, err := range service.SubscribeToTask(ctx, store.instance.Id, &a2a.SubscribeToTaskRequest{ID: "task"}, &testUpdates{err: test.ingestionErr}) {
				if err != nil {
					lastErr = err
					continue
				}
				frames++
				require.Equal(t, a2a.TaskStateWorking, event.(*a2a.Task).Status.State)
			}
			require.ErrorIs(t, lastErr, test.want)
			require.Equal(t, 1, frames)
		})
	}
}

func TestSubscriptionCancellationAndEarlyClose(t *testing.T) {
	for _, early := range []bool{false, true} {
		ctx, store, service := subscriptionFixture(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		var lastErr error
		for _, err := range service.SubscribeToTask(ctx, store.instance.Id, &a2a.SubscribeToTaskRequest{ID: "task"}, &testUpdates{changed: make(chan struct{})}) {
			if err != nil {
				lastErr = err
				break
			}
			if early {
				break
			}
			cancel()
		}
		if early {
			require.Zero(t, store.reads)
		} else {
			require.ErrorIs(t, lastErr, context.Canceled)
		}
	}
}
