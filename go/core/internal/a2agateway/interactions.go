package a2agateway

import (
	"context"
	"errors"
	"iter"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"k8s.io/apimachinery/pkg/types"
)

// runtimeDrainTimeout bounds the wait for a terminal stream to finish exporting
// its request telemetry before the gateway closes the observation connection.
const runtimeDrainTimeout = 2 * time.Second

func (g *Gateway) cancelTask(ctx context.Context, agent types.NamespacedName, req *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	if req == nil {
		return nil, a2atype.ErrInvalidParams
	}
	session, task, err := g.interactions.PrepareCancelTask(ctx, agent, req)
	if err != nil {
		return nil, err
	}
	if task.Status.State.Terminal() {
		return task, nil
	}
	client, err := g.dial(ctx, session)
	if err != nil {
		return nil, err
	}
	closeRuntime := sync.OnceValue(client.Destroy)
	defer closeRuntime()
	result, err := client.CancelTask(ctx, req)
	if err != nil {
		_ = closeRuntime()
		if ctx.Err() == nil {
			if recovered, recoverErr := recoverBoundary(g.interactions.GetCancelResult(ctx, agent, req.ID)); recoverErr != nil || (recovered != nil && recovered.Status.State.Terminal()) {
				return recovered, recoverErr
			}
		}
		return nil, err
	}
	// Release this observation connection before waiting for native cleanup.
	if err := closeRuntime(); err != nil {
		return nil, err
	}
	if result != nil && isQuiescent(result.Status.State) {
		return g.interactions.GetCancelResult(ctx, agent, req.ID)
	}
	return result, nil
}

func (g *Gateway) sendMessage(ctx context.Context, agent types.NamespacedName, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	var historyLength *int
	if req != nil && req.Config != nil {
		historyLength = req.Config.HistoryLength
	}
	prepared, err := g.interactions.PrepareSend(ctx, agent, req)
	if err != nil {
		return nil, err
	}
	session, dispatchID := prepared.Session, prepared.DispatchID
	if task := prepared.AcceptedTask; task != nil {
		if req.Config != nil && req.Config.ReturnImmediately {
			return task, nil
		}
		return g.awaitTaskCompletion(ctx, agent, req.Message, task.ID, historyLength)
	}
	ctx = a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{apia2a.DispatchHeader: {dispatchID.String()}})
	defer g.finishSend(ctx, agent, req.Message, dispatchID, nil)
	client, err := g.dial(ctx, session)
	if err != nil {
		return nil, err
	}
	closeRuntime := sync.OnceValue(client.Destroy)
	defer closeRuntime()
	result, err := client.SendMessage(ctx, req)
	if err != nil {
		err = g.finishSend(ctx, agent, req.Message, dispatchID, err)
		_ = closeRuntime()
		// Native settlement can pause the runtime before its unary response is
		// delivered. Recover only this accepted input's durable boundary.
		// The gRPC SDK maps proxy connection failures to A2A InternalError.
		// Preserve explicit protocol rejections, including unwrapped sentinels.
		if ctx.Err() == nil && a2atype.ErrorReason(err) == a2atype.ErrorReason(a2atype.ErrInternalError) {
			if task, readErr := g.interactions.GetTaskByMessage(ctx, agent, req.Message); readErr == nil {
				if recovered, recoverErr := recoverBoundary(g.interactions.GetSendResult(ctx, agent, req.Message, task.ID, historyLength)); recovered != nil || recoverErr != nil {
					return recovered, recoverErr
				}
			}
		}
		return nil, err
	}
	if task, ok := result.(*a2atype.Task); ok && isQuiescent(task.Status.State) {
		if err := closeRuntime(); err != nil {
			return nil, err
		}
		stored, err := g.interactions.GetSendResult(ctx, agent, req.Message, task.ID, historyLength)
		if err != nil {
			return nil, err
		}
		return stored, nil
	}
	return result, nil
}

func (g *Gateway) subscribeToTask(ctx context.Context, agent types.NamespacedName, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	if req == nil {
		return errorEvents(a2atype.ErrInvalidParams)
	}
	session, task, err := g.interactions.PrepareTaskSubscription(ctx, agent, req)
	if err != nil {
		return errorEvents(err)
	}
	if isQuiescent(task.Status.State) {
		return func(yield func(a2atype.Event, error) bool) { yield(task, nil) }
	}
	client, err := g.dial(ctx, session)
	if err != nil {
		return errorEvents(err)
	}
	// The SDK subscription supplies its own initial task and later events.
	// Do not concatenate an unrelated stored snapshot with that live stream.
	return g.observe(ctx, agent, session, req.ID, nil, nil, client, client.SubscribeToTask(ctx, req))
}

func (g *Gateway) sendStreamingMessage(ctx context.Context, agent types.NamespacedName, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		// Reserve only when the caller starts consuming: an unused iterator must
		// not block lifecycle work with an undispatched reservation.
		prepared, err := g.interactions.PrepareSend(ctx, agent, req)
		if err != nil {
			yield(nil, err)
			return
		}
		session, dispatchID := prepared.Session, prepared.DispatchID
		var historyLength *int
		if req.Config != nil {
			historyLength = req.Config.HistoryLength
		}
		if task := prepared.AcceptedTask; task != nil {
			if isQuiescent(task.Status.State) {
				yield(task, nil)
				return
			}
			g.subscribeToTask(ctx, agent, &a2atype.SubscribeToTaskRequest{ID: task.ID})(yield)
			return
		}
		ctx := a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{apia2a.DispatchHeader: {dispatchID.String()}})
		defer g.finishSend(ctx, agent, req.Message, dispatchID, nil)
		client, err := g.dial(ctx, session)
		if err != nil {
			yield(nil, err)
			return
		}
		events := func(next func(a2atype.Event, error) bool) {
			for event, err := range client.SendStreamingMessage(ctx, req) {
				if err != nil {
					next(nil, g.finishSend(ctx, agent, req.Message, dispatchID, err))
					return
				}
				if !next(event, nil) {
					return
				}
			}
			next(nil, g.finishSend(ctx, agent, req.Message, dispatchID, a2atype.ErrInternalError))
		}
		g.observe(ctx, agent, session, req.Message.TaskID, req.Message, historyLength, client, events)(yield)
	}
}

// finishSend releases the attempt even after the caller disconnects. A failed
// actor call is retryable only if persistence proves the input was not accepted.
func (g *Gateway) finishSend(ctx context.Context, agent types.NamespacedName, message *a2atype.Message, id uuid.UUID, sendErr error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	revoked, err := g.interactions.RevokeSend(ctx, agent, message, id)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "release runtime dispatch", "session_id", message.ContextID, "error", err)
	} else if revoked && sendErr != nil && a2atype.ErrorReason(sendErr) == a2atype.ErrorReason(a2atype.ErrInternalError) {
		return sessionsvc.ErrSendNotAccepted
	}
	return sendErr
}

// observe owns only this observer's actor connection. Disconnecting cannot
// cancel execution. Final results come from the service after native cleanup
// acknowledges their saved version, independently of actor pause or suspend.
func (g *Gateway) observe(ctx context.Context, agent types.NamespacedName, session *apiv1alpha1.Session, taskID a2atype.TaskID, message *a2atype.Message, historyLength *int, client *a2aclient.Client, events iter.Seq2[a2atype.Event, error]) iter.Seq2[a2atype.Event, error] {
	// Keep the original operation's permissions when reading its result. The
	// actor may assign a task ID, but that does not turn a send into an update.
	readResult := func() (*a2atype.Task, error) {
		if message != nil {
			return g.interactions.GetSendResult(ctx, agent, message, taskID, historyLength)
		}
		return g.interactions.GetSettledTask(ctx, agent, session.Id, taskID, historyLength)
	}
	return func(yield func(a2atype.Event, error) bool) {
		closeRuntime := sync.OnceValue(client.Destroy)
		next, stop := iter.Pull2(events)
		defer stop()
		defer closeRuntime()
		var streamErr error
		for {
			event, err, ok := next()
			if !ok {
				break
			}
			if err != nil {
				streamErr = err
				break
			}
			if event == nil || event.TaskInfo().TaskID == "" || event.TaskInfo().ContextID != session.ContextId || (taskID != "" && event.TaskInfo().TaskID != taskID) {
				yield(nil, a2atype.NewError(a2atype.ErrInternalError, "runtime returned an unexpected task"))
				return
			}
			taskID = event.TaskInfo().TaskID
			var state a2atype.TaskState
			switch value := event.(type) {
			case *a2atype.Task:
				state = value.Status.State
			case *a2atype.TaskStatusUpdateEvent:
				state = value.Status.State
			}
			if isQuiescent(state) {
				if state.Terminal() {
					// Let the runtime finish its response naturally. A stuck stream
					// must not prevent delivery of the persisted task result.
					timer := time.AfterFunc(runtimeDrainTimeout, func() { _ = closeRuntime() })
					for {
						if _, _, ok := next(); !ok {
							break
						}
					}
					timer.Stop()
				}
				if err := closeRuntime(); err != nil {
					yield(nil, err)
					return
				}
				task, err := readResult()
				yield(task, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
		// Finish can race a subscription attach or stop the runtime stream.
		// Recover only durable public state; an incomplete stream is an error.
		if message != nil && ctx.Err() == nil {
			// A continuation's previous waiting boundary is not its response.
			task, err := g.interactions.GetTaskByMessage(ctx, agent, message)
			if err == nil {
				taskID = task.ID
			} else {
				taskID = ""
			}
		}
		if taskID != "" && ctx.Err() == nil && (streamErr == nil || a2atype.ErrorReason(streamErr) == a2atype.ErrorReason(a2atype.ErrInternalError) || (message == nil && errors.Is(streamErr, a2atype.ErrTaskNotFound))) {
			_ = closeRuntime()
			if task, err := recoverBoundary(readResult()); task != nil || err != nil {
				yield(task, err)
				return
			}
		}
		if streamErr == nil {
			streamErr = a2atype.NewError(a2atype.ErrInternalError, "runtime stream ended before a task boundary")
		}
		yield(nil, streamErr)
	}
}

// recoverBoundary recovers only a published waiting or terminal result after
// losing an actor connection. A running task must not look completed.
func recoverBoundary(task *a2atype.Task, err error) (*a2atype.Task, error) {
	if err == nil && isQuiescent(task.Status.State) {
		return task, nil
	}
	if err != nil && !errors.Is(err, a2atype.ErrTaskNotFound) {
		return nil, err
	}
	return nil, nil
}

func (g *Gateway) dial(ctx context.Context, session *apiv1alpha1.Session) (*a2aclient.Client, error) {
	client, err := g.dialer.Dial(ctx, session)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "connect to runtime", "session_id", session.Id, "error", err)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to Session runtime")
	}
	return client, nil
}

func (g *Gateway) awaitTaskCompletion(ctx context.Context, agent types.NamespacedName, message *a2atype.Message, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		task, err := g.interactions.GetSendResult(ctx, agent, message, taskID, historyLength)
		if err != nil {
			return nil, err
		}
		if isQuiescent(task.Status.State) {
			return task, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func requiresInput(state a2atype.TaskState) bool {
	return state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

func isQuiescent(state a2atype.TaskState) bool {
	return state.Terminal() || requiresInput(state)
}

func errorEvents(err error) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		var zero a2atype.Event
		yield(zero, err)
	}
}
