package a2agateway

import (
	"context"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstancetask"
	"github.com/stretchr/testify/require"
)

func TestPostgresSlowObserverDoesNotBlockIngestion(t *testing.T) {
	store, instance := gatewayPostgresFixture(t)
	ctx, cancel := context.WithTimeout(gatewayTestContext(), 20*time.Second)
	defer cancel()
	task := &a2atype.Task{ID: "observed", ContextID: instance.ContextId, Status: a2atype.TaskStatus{State: a2atype.TaskStateSubmitted}}
	task.History = []*a2atype.Message{a2atype.NewMessageForTask(a2atype.MessageRoleUser, task, a2atype.NewTextPart("start"))}
	request := &a2atype.SendMessageRequest{Message: task.History[0]}
	admission, err := store.AdmitAgentInstanceTask(ctx, instance.Id, []byte("request"), request, task)
	require.NoError(t, err)
	ownerID := uuid.New()
	claim, claimed, err := store.ClaimAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, ownerID, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	gateway := &Gateway{
		store: store, tasks: agentinstancetask.NewService(store, &gatewayTestAuthorizer{}),
		workflow: &gatewayTestWorkflow{}, turnLease: time.Minute, turnRenewal: time.Second,
	}
	produce := make(chan struct{})
	events := func(yield func(a2atype.Event, error) bool) {
		select {
		case <-produce:
		case <-ctx.Done():
			return
		}
		for i := range 140 {
			if !yield(&a2atype.TaskArtifactUpdateEvent{
				TaskID: task.ID, ContextID: task.ContextID, Append: i > 0, LastChunk: i == 139,
				Artifact: &a2atype.Artifact{ID: "reply", Parts: a2atype.ContentParts{a2atype.NewTextPart("chunk")}},
			}, nil) {
				return
			}
		}
		yield(a2atype.NewMessageForTask(a2atype.MessageRoleAgent, task, a2atype.NewTextPart("finished")), nil)
	}
	attempt := &preparedSend{instance: instance, task: claim.Task, request: claim.Request, turnID: admission.TurnID, ownerID: ownerID, claimed: true}
	run, err := gateway.startOwnedTaskRun(ctx, attempt, gatewayTestClient(t, &gatewayTestRuntime{}))
	require.NoError(t, err)
	run.startIngest(events)
	initial := make(chan struct{})
	releaseReader := make(chan struct{}, 1)
	defer close(releaseReader)
	type result struct {
		task    *a2atype.Task
		appends int
		err     error
	}
	observed := make(chan result, 1)
	go func() {
		var got result
		for event, err := range run.observe(ctx) {
			if err != nil {
				got.err = err
				break
			}
			if got.task == nil {
				got.task = event.(*a2atype.Task)
				close(initial)
				select {
				case <-releaseReader:
				case <-ctx.Done():
					got.err = ctx.Err()
				}
			} else {
				got.task, got.err = a2aevent.ApplyUpdate(got.task, event)
			}
			if got.err != nil {
				break
			}
			if update, ok := event.(*a2atype.TaskArtifactUpdateEvent); ok && update.Append {
				got.appends++
			}
		}
		observed <- got
	}()
	select {
	case <-initial:
	case <-ctx.Done():
		t.Fatal("observer did not read initial snapshot")
	}
	close(produce)
	select {
	case <-run.done:
	case <-ctx.Done():
		t.Fatal("a stalled observer blocked ingestion")
	}
	_, ingestionErr := run.Changes()
	require.NoError(t, ingestionErr)
	stored, err := store.GetAgentInstanceTask(ctx, instance.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2atype.TaskStateCompleted, stored.Status.State)
	require.Len(t, stored.History, 2)
	require.Len(t, stored.Artifacts[0].Parts, 140)
	// Release without closing, so the deferred close still protects failure paths.
	releaseReader <- struct{}{}
	select {
	case got := <-observed:
		require.NoError(t, got.err)
		require.Equal(t, stored, got.task)
		require.Equal(t, 139, got.appends, "catch-up applies each append exactly once")
	case <-ctx.Done():
		t.Fatal("observer did not catch up")
	}
	// Reconnecting after the ingester is gone needs only the durable snapshot.
	frames := 0
	for event, err := range gateway.tasks.SubscribeToTask(ctx, instance.Id, &a2atype.SubscribeToTaskRequest{ID: task.ID}, nil) {
		require.NoError(t, err)
		require.Equal(t, stored, event)
		frames++
	}
	require.Equal(t, 1, frames)
}
