package a2agateway

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstancetask"
)

// taskRun is the local representative of one durably owned turn. PostgreSQL
// authorizes runtime effects; this object serializes those effects on one replica
// and emits payload-free notifications after their database commits.
type taskRun struct {
	gateway  *Gateway
	instance *apiv1alpha1.AgentInstance
	client   *a2aclient.Client
	key      string
	turnID   uuid.UUID
	ownerID  uuid.UUID
	done     chan struct{}
	cancel   chan struct{}
	ctx      context.Context

	controlCancel context.CancelFunc
	closeOnce     sync.Once
	closeErr      error
	finishOnce    sync.Once
	effectMu      sync.Mutex

	mu      sync.Mutex
	task    *a2atype.Task
	err     error
	changed chan struct{}
}

var _ agentinstancetask.TaskUpdates = (*taskRun)(nil)

func taskRunKey(instanceID string, taskID a2atype.TaskID) string {
	return instanceID + "/" + string(taskID)
}

func (g *Gateway) taskRun(instanceID string, taskID a2atype.TaskID) (*taskRun, bool) {
	run, ok := g.runs.Load(taskRunKey(instanceID, taskID))
	if !ok {
		return nil, false
	}
	return run.(*taskRun), true
}

// startOwnedTaskRun installs local coordination before recording ISSUED. If local
// setup fails, the still-unissued database claim remains safe to release.
func (g *Gateway) startOwnedTaskRun(ctx context.Context, attempt *preparedSend, client *a2aclient.Client) (*taskRun, error) {
	run, err := g.newTaskRun(ctx, attempt, client)
	if err != nil {
		g.releaseUnissued(ctx, attempt)
		_ = client.Destroy()
		return nil, err
	}
	if err := g.store.MarkAgentInstanceTaskTurnIssued(ctx, attempt.instance.GetId(), string(attempt.task.ID), attempt.turnID, attempt.ownerID); err != nil {
		g.releaseUnissued(ctx, attempt)
		run.finish(context.WithoutCancel(ctx))
		return nil, err
	}
	go run.control(run.ctx)
	return run, nil
}

func (g *Gateway) newTaskRun(ctx context.Context, attempt *preparedSend, client *a2aclient.Client) (*taskRun, error) {
	key := taskRunKey(attempt.instance.GetId(), attempt.task.ID)
	controlCtx, controlCancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &taskRun{
		gateway: g, instance: attempt.instance, client: client, key: key,
		turnID: attempt.turnID, ownerID: attempt.ownerID, task: attempt.task,
		done: make(chan struct{}), cancel: make(chan struct{}, 1), changed: make(chan struct{}),
		ctx: controlCtx, controlCancel: controlCancel,
	}
	if existing, loaded := g.runs.LoadOrStore(key, run); loaded {
		controlCancel()
		other := existing.(*taskRun)
		if other.turnID != attempt.turnID {
			return nil, fmt.Errorf("different task turn ingester already exists")
		}
		return nil, fmt.Errorf("task turn ingester already exists")
	}
	return run, nil
}

func (r *taskRun) startIngest(events iter.Seq2[a2atype.Event, error]) {
	go r.ingest(r.ctx, events)
}

func (r *taskRun) ingest(ctx context.Context, events iter.Seq2[a2atype.Event, error]) {
	for event, eventErr := range events {
		if eventErr != nil {
			r.fail(ctx, eventErr)
			return
		}
		if err := r.processEvent(ctx, event); err != nil {
			r.fail(ctx, r.gateway.storeError(ctx, err))
			return
		}
		if r.isDone() {
			return
		}
	}
	if !r.isDone() {
		r.fail(ctx, a2atype.NewError(a2atype.ErrServerError, "runtime event stream ended before the task became quiescent"))
	}
}

func (r *taskRun) control(ctx context.Context) {
	ticker := time.NewTicker(r.gateway.renewalInterval())
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ctx.Done():
			return
		case <-r.cancel:
			r.cancelRuntime(ctx)
		case <-ticker.C:
			cancelRequested, err := r.gateway.store.RenewAgentInstanceTaskTurn(ctx, r.instance.GetId(), string(r.taskID()), r.turnID, r.ownerID, r.gateway.leaseDuration())
			if err != nil {
				r.fail(ctx, r.gateway.storeError(ctx, err))
				return
			}
			if cancelRequested {
				r.cancelRuntime(ctx)
			}
		}
	}
}

func (r *taskRun) signalCancel() {
	select {
	case r.cancel <- struct{}{}:
	default:
	}
}

func (r *taskRun) cancelRuntime(ctx context.Context) {
	r.effectMu.Lock()
	defer r.effectMu.Unlock()
	if r.isDone() || isQuiescent(r.currentTask().Status.State) {
		return
	}
	began, err := r.gateway.store.BeginAgentInstanceTaskCancellation(ctx, r.instance.GetId(), string(r.taskID()), r.turnID, r.ownerID)
	if err != nil {
		r.setError(r.gateway.storeError(ctx, err))
		r.finish(ctx)
		return
	}
	if !began {
		return
	}
	canceled, err := r.client.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: r.taskID()})
	if err != nil {
		// CANCELING records the ambiguous external call. Do not automatically retry it.
		return
	}
	if canceled == nil || validateTaskInfo(canceled, r.currentTask()) != nil || !canceled.Status.State.Terminal() {
		return
	}
	if err := r.processEventLocked(ctx, canceled); err != nil {
		r.setError(r.gateway.storeError(ctx, err))
		r.finish(ctx)
	}
}

func (r *taskRun) processEvent(ctx context.Context, event a2atype.Event) error {
	r.effectMu.Lock()
	defer r.effectMu.Unlock()
	return r.processEventLocked(ctx, event)
}

func (r *taskRun) processEventLocked(ctx context.Context, event a2atype.Event) error {
	if r.isDone() {
		return nil
	}
	updated, err := taskForEvent(r.currentTask(), event)
	if err != nil {
		return err
	}
	return r.processUpdatedEventLocked(ctx, updated, event)
}

func (r *taskRun) processResult(ctx context.Context, result a2atype.SendMessageResult) error {
	r.effectMu.Lock()
	defer r.effectMu.Unlock()
	updated, err := taskForResult(r.currentTask(), result)
	if err != nil {
		return err
	}
	return r.processUpdatedEventLocked(ctx, updated, result)
}

func (r *taskRun) processUpdatedEventLocked(ctx context.Context, updated *a2atype.Task, event a2atype.Event) error {
	if !isQuiescent(updated.Status.State) {
		if err := r.gateway.store.StoreOwnedAgentInstanceTaskEvent(ctx, r.instance.GetId(), r.turnID, r.ownerID, updated, event); err != nil {
			return err
		}
		r.setTask(updated)
		r.notifyCommit()
		return nil
	}
	if err := r.gateway.store.BeginAgentInstanceTaskQuiescence(ctx, r.instance.GetId(), string(updated.ID), r.turnID, r.ownerID, updated.Status.State); err != nil {
		return err
	}
	var snapshot *database.AgentInstanceTaskSnapshot
	var err error
	if updated.Status.State.Terminal() {
		if err := r.closeRuntime(); err != nil {
			return fmt.Errorf("close terminal runtime stream: %w", err)
		}
		snapshot, err = r.gateway.workflow.Quiesce(ctx, r.instance)
	} else {
		err = r.gateway.workflow.Pause(ctx, r.instance)
	}
	if err != nil {
		return fmt.Errorf("quiesce AgentInstance runtime: %w", err)
	}
	if err := r.gateway.store.SettleAgentInstanceTaskTurn(ctx, r.instance.GetId(), r.turnID, r.ownerID, updated, event, snapshot); err != nil {
		return err
	}
	r.setTask(updated)
	r.notifyCommit()
	r.finish(ctx)
	return nil
}

// Cancellation and terminal ingestion can both close ingress. Share the result so
// grpc.ClientConn.Close is called exactly once.
func (r *taskRun) closeRuntime() error {
	r.closeOnce.Do(func() { r.closeErr = r.client.Destroy() })
	return r.closeErr
}

func (r *taskRun) finish(_ context.Context) {
	r.finishOnce.Do(func() {
		r.controlCancel()
		_ = r.closeRuntime()
		close(r.done)
		r.mu.Lock()
		close(r.changed)
		r.changed = nil
		r.mu.Unlock()
		r.gateway.runs.CompareAndDelete(r.key, r)
	})
}

// fail serializes shutdown with runtime effects and event persistence. Callers that
// already hold effectMu must set the error and finish directly.
func (r *taskRun) fail(ctx context.Context, err error) {
	r.effectMu.Lock()
	defer r.effectMu.Unlock()
	if r.isDone() {
		return
	}
	r.setError(err)
	r.finish(ctx)
}

func (r *taskRun) observe(ctx context.Context) iter.Seq2[a2atype.Event, error] {
	return r.gateway.tasks.SubscribeToTask(ctx, r.instance.GetId(), &a2atype.SubscribeToTaskRequest{ID: r.taskID()}, r)
}

// Changes snapshots the next commit notification before an observer reads the log.
// Closing a channel broadcasts progress without buffering payloads or blocking ingestion.
func (r *taskRun) Changes() (<-chan struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.changed == nil {
		return nil, r.err
	}
	return r.changed, nil
}

func (r *taskRun) notifyCommit() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.changed != nil {
		close(r.changed)
		r.changed = make(chan struct{})
	}
}

func (r *taskRun) taskID() a2atype.TaskID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.task.ID
}

func (r *taskRun) currentTask() *a2atype.Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.task
}

func (r *taskRun) setTask(task *a2atype.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.task = task
}

func (r *taskRun) isDone() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *taskRun) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}
