package a2agateway

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// taskRun is the single owner of task event persistence and runtime quiescence.
// Public streams only observe the events it publishes.
type taskRun struct {
	gateway   *Gateway
	client    *a2aclient.Client
	closeOnce sync.Once
	closeErr  error
	key       string
	queueID   a2atype.TaskID
	done      chan struct{}

	mu   sync.Mutex
	err  error
	last a2atype.Event
}

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

func (g *Gateway) startTaskRun(ctx context.Context, instance *apiv1alpha1.AgentInstance, task *a2atype.Task, client *a2aclient.Client, events iter.Seq2[a2atype.Event, error]) (*taskRun, eventqueue.Reader, error) {
	key := taskRunKey(instance.GetId(), task.ID)
	run := &taskRun{gateway: g, client: client, key: key, queueID: a2atype.TaskID(key), done: make(chan struct{})}
	if _, loaded := g.runs.LoadOrStore(key, run); loaded {
		return nil, nil, fmt.Errorf("task event ingester already exists")
	}
	writer, err := g.events.CreateWriter(ctx, run.queueID)
	if err != nil {
		g.runs.Delete(key)
		return nil, nil, fmt.Errorf("create task event publisher: %w", err)
	}
	reader, err := g.events.CreateReader(ctx, run.queueID)
	if err != nil {
		_ = writer.Close()
		_ = g.events.Destroy(ctx, run.queueID)
		g.runs.Delete(key)
		return nil, nil, fmt.Errorf("create task event reader: %w", err)
	}
	go run.ingest(context.WithoutCancel(ctx), instance, task, writer, events)
	return run, reader, nil
}

// Cancellation and terminal ingestion can both close ingress. Share the
// result so grpc.ClientConn.Close is called exactly once.
func (r *taskRun) closeRuntime() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.client.Destroy()
	})
	return r.closeErr
}

// runtimeDrainTimeout bounds how long a terminal turn waits for the runtime to
// end its stream before the gateway closes it.
var runtimeDrainTimeout = 2 * time.Second

func (r *taskRun) ingest(ctx context.Context, instance *apiv1alpha1.AgentInstance, task *a2atype.Task, writer eventqueue.Writer, events iter.Seq2[a2atype.Event, error]) {
	next, stop := iter.Pull2(events)
	defer func() {
		_ = writer.Close()
		_ = r.closeRuntime()
		stop()
		close(r.done)
		_ = r.gateway.events.Destroy(ctx, r.queueID)
		r.gateway.runs.CompareAndDelete(r.key, r)
	}()

	for {
		event, eventErr, ok := next()
		if !ok {
			return
		}
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
				r.drainRuntime(next)
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
		if err := writer.Write(ctx, &eventqueue.Message{Event: event}); err != nil {
			r.setError(r.gateway.storeError(ctx, fmt.Errorf("publish task event: %w", err)))
			return
		}
		r.setLast(event)
		task = updated
		if isQuiescent(task.Status.State) {
			return
		}
	}
}

// drainRuntime reads the runtime stream to its end, so the call completes
// instead of being canceled. A runtime that keeps the stream open past the
// timeout is closed. next is not used again once this returns.
func (r *taskRun) drainRuntime(next func() (a2atype.Event, error, bool)) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, ok := next(); !ok {
				return
			}
		}
	}()
	timer := time.NewTimer(runtimeDrainTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		_ = r.closeRuntime()
		<-done
	}
}

func (r *taskRun) observe(ctx context.Context, initial a2atype.Event) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		select {
		case <-r.done:
			if event := r.getLast(); event != nil {
				yield(event, nil)
			} else if initial != nil {
				yield(initial, nil)
			}
			if err := r.getError(); err != nil {
				yield(nil, err)
			}
			return
		default:
		}
		reader, err := r.gateway.events.CreateReader(ctx, r.queueID)
		if err != nil {
			yield(nil, err)
			return
		}
		for event, err := range r.observeReader(ctx, initial, reader) {
			if !yield(event, err) {
				return
			}
		}
	}
}

func (r *taskRun) observeReader(ctx context.Context, initial a2atype.Event, reader eventqueue.Reader) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		defer reader.Close()
		if initial != nil && !yield(initial, nil) {
			return
		}
		received := false
		for {
			message, err := reader.Read(ctx)
			if errors.Is(err, eventqueue.ErrQueueClosed) {
				if !received {
					if event := r.getLast(); event != nil && !yield(event, nil) {
						return
					}
				}
				if runErr := r.getError(); runErr != nil {
					yield(nil, runErr)
				}
				return
			}
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(message.Event, nil) {
				return
			}
			received = true
		}
	}
}

func (r *taskRun) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *taskRun) getError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *taskRun) setLast(event a2atype.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = event
}

func (r *taskRun) getLast() a2atype.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}
