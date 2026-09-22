package a2agateway

import (
	"context"
	"fmt"
	"iter"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/agentinstancetask"
)

// taskRun is the single owner of task event persistence and runtime quiescence.
// Public streams observe committed records; notifications never wait for readers.
type taskRun struct {
	gateway    *Gateway
	client     *a2aclient.Client
	closeOnce  sync.Once
	closeErr   error
	key        string
	instanceID string
	taskID     a2atype.TaskID
	done       chan struct{}

	mu      sync.Mutex
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

func (g *Gateway) startTaskRun(ctx context.Context, instance *apiv1alpha1.AgentInstance, task *a2atype.Task, client *a2aclient.Client, events iter.Seq2[a2atype.Event, error]) (*taskRun, error) {
	key := taskRunKey(instance.GetId(), task.ID)
	run := &taskRun{gateway: g, client: client, key: key, instanceID: instance.Id, taskID: task.ID, done: make(chan struct{}), changed: make(chan struct{})}
	if _, loaded := g.runs.LoadOrStore(key, run); loaded {
		return nil, fmt.Errorf("task event ingester already exists")
	}
	go run.ingest(context.WithoutCancel(ctx), instance, task, events)
	return run, nil
}

// Cancellation and terminal ingestion can both close ingress. Share the
// result so grpc.ClientConn.Close is called exactly once.
func (r *taskRun) closeRuntime() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.client.Destroy()
	})
	return r.closeErr
}

func (r *taskRun) ingest(ctx context.Context, instance *apiv1alpha1.AgentInstance, task *a2atype.Task, events iter.Seq2[a2atype.Event, error]) {
	defer func() {
		_ = r.closeRuntime()
		close(r.done)
		r.mu.Lock()
		close(r.changed)
		r.changed = nil
		r.mu.Unlock()
		r.gateway.runs.CompareAndDelete(r.key, r)
	}()

	for event, eventErr := range events {
		if eventErr != nil {
			r.setError(eventErr)
			return
		}
		updated, err := taskForEvent(task, event)
		if err == nil && isQuiescent(updated.Status.State) {
			release := r.gateway.coordinator.Quiesce(instance.GetId())
			// Terminal suspension must close the runtime stream so it cannot wait on
			// itself. Input pauses checkpoint the still-live request first.
			if updated.Status.State.Terminal() {
				if closeErr := r.closeRuntime(); closeErr != nil {
					err = fmt.Errorf("close terminal runtime stream: %w", closeErr)
				}
			}
			if err == nil {
				err = r.gateway.storeEvent(ctx, instance, updated, event)
			}
			release()
		} else if err == nil {
			err = r.gateway.storeEvent(ctx, instance, updated, event)
		}
		if err != nil {
			r.setError(r.gateway.storeError(ctx, err))
			return
		}
		r.mu.Lock()
		close(r.changed)
		r.changed = make(chan struct{})
		r.mu.Unlock()
		task = updated
		if isQuiescent(task.Status.State) {
			return
		}
	}
}

func (r *taskRun) observe(ctx context.Context) iter.Seq2[a2atype.Event, error] {
	return r.gateway.tasks.SubscribeToTask(ctx, r.instanceID, &a2atype.SubscribeToTaskRequest{ID: r.taskID}, r)
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

func (r *taskRun) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}
