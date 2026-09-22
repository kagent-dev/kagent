package agentinstancetask

import (
	"context"
	"errors"
	"iter"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	corea2a "github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// TaskUpdates observes an existing ingester without starting or owning execution.
// Changes returns a channel closed by the next commit or ingester exit. Call it
// before reading the log so a concurrent commit cannot be missed. A nil channel
// means ingestion has ended; the error describes its failure, if any. Notifications
// carry no payload and may coalesce because committed records remain in the store.
type TaskUpdates interface {
	Changes() (<-chan struct{}, error)
}

// SubscribeToTask restores the caller's task/history, then streams committed
// updates from the same boundary. It consumes an existing ingester's notifications;
// it never starts runtime work. A nil source can serve a completed/waiting task,
// but an active task without ingestion returns an error after its current snapshot.
// Slow consumers retain only a bounded page and their task state, never an event queue.
func (s *Service) SubscribeToTask(ctx context.Context, instanceID string, req *a2atype.SubscribeToTaskRequest, updates TaskUpdates) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		instance, err := s.Instance(ctx, instanceID, auth.VerbGet)
		if err != nil {
			yield(nil, err)
			return
		}
		if req == nil || req.ID == "" {
			yield(nil, a2atype.NewError(a2atype.ErrInvalidRequest, "task ID is required"))
			return
		}
		observation, err := s.store.GetAgentInstanceTaskObservation(ctx, instance.Id, string(req.ID), nil)
		if err != nil {
			yield(nil, subscriptionError(ctx, err))
			return
		}
		task, err := pbconv.ToProtoTask(observation.Task)
		if err != nil {
			yield(nil, subscriptionError(ctx, err))
			return
		}
		state := observation.Task.Status.State
		if !yield(observation.Task, nil) || quiescent(state) {
			return
		}
		history := task.History
		task.History = nil
		snapshot := func() (*a2atype.Task, error) {
			task.History = history
			defer func() { task.History = nil }()
			return pbconv.FromProtoTask(task)
		}
		position := observation.Sequence
		const pageSize = 128
		for {
			var changed <-chan struct{}
			var ingestionErr error
			if updates != nil {
				changed, ingestionErr = updates.Changes()
			}
			records, err := s.store.ListAgentInstanceTaskEvents(ctx, instance.Id, string(req.ID), position, pageSize)
			if err != nil {
				yield(nil, subscriptionError(ctx, err))
				return
			}
			for _, record := range records {
				if err := ctx.Err(); err != nil {
					yield(nil, err)
					return
				}
				event, err := pbconv.ToProtoStreamResponse(record.Event)
				if err == nil {
					task, err = corea2a.ApplyTaskEvent(task, event)
				}
				if err != nil {
					yield(nil, subscriptionError(ctx, err))
					return
				}
				position = record.Sequence
				if message := event.GetMessage(); message != nil {
					history = append(history, message)
					// Archive rows accompany task transitions. Retain them for the
					// next snapshot; publishing them would repeat live status chunks.
					continue
				}
				switch update := record.Event.(type) {
				case *a2atype.Task:
					state = update.Status.State
				case *a2atype.TaskStatusUpdateEvent:
					state = update.Status.State
				}
				// A final status may precede its archived output in the same commit,
				// even across pages. Drain the log before publishing that snapshot.
				if quiescent(state) {
					continue
				}
				output := record.Event
				if event.GetTask() != nil {
					output, err = snapshot()
					if err != nil {
						yield(nil, subscriptionError(ctx, err))
						return
					}
				}
				if !yield(output, nil) {
					return
				}
			}
			if len(records) == pageSize {
				continue
			}
			if quiescent(state) {
				current, err := snapshot()
				yield(current, subscriptionError(ctx, err))
				return
			}
			if changed == nil {
				if ingestionErr == nil {
					ingestionErr = a2atype.NewError(a2atype.ErrInternalError, "task observation is unavailable; reconnect to recover committed progress")
				}
				yield(nil, ingestionErr)
				return
			}
			select {
			case <-changed:
			case <-ctx.Done():
				yield(nil, ctx.Err())
				return
			}
		}
	}
}

func quiescent(state a2atype.TaskState) bool {
	return state.Terminal() || state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

func subscriptionError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, database.ErrNotFound) {
		return a2atype.ErrTaskNotFound
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	logging.FromContext(ctx).ErrorContext(ctx, "failed to read task subscription", "error", err)
	return a2atype.NewError(a2atype.ErrInternalError, "failed to read task subscription")
}
