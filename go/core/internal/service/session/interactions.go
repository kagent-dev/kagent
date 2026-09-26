package session

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"iter"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

type interactionStore interface {
	ReserveSessionDispatch(context.Context, string, uuid.UUID, string) error
	RevokeSessionDispatch(context.Context, string, uuid.UUID, string) (bool, error)
	GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
	GetSessionTaskByMessage(context.Context, string, string, string) (*a2atype.Task, error)
	GetSessionTask(context.Context, string, string, *int) (*a2atype.Task, error)
	GetSettledSessionTask(context.Context, string, string, *int) (*a2atype.Task, error)
	ListAgentTasks(context.Context, []string, string, a2atype.TaskState, *time.Time, int, *int) ([]*a2atype.Task, int, error)
	SessionForTask(context.Context, string) (string, error)
}

type agentService interface {
	Get(context.Context, types.NamespacedName) (*v1alpha3.Agent, error)
}

type runtimeDialer interface {
	Dial(context.Context, *apiv1alpha1.Session) (*a2aclient.Client, error)
}

// InteractionService owns public conversation operations, including authorization,
// dispatch, and observation of committed task state. The selected Agent is explicit;
// transport routing metadata never determines which Session it may access.
// Session access policy stays private and is shared with lifecycle operations.
// Observers never become runtime task writers or executors.
type InteractionService struct {
	store    interactionStore
	dialer   runtimeDialer
	agents   agentService
	sessions *Service
}

func NewInteractionService(store interactionStore, dialer runtimeDialer, agents agentService, sessions *Service) *InteractionService {
	return &InteractionService{store: store, dialer: dialer, agents: agents, sessions: sessions}
}

// runtimeDrainTimeout bounds the wait for a terminal stream to finish exporting
// its request telemetry before the observer closes the runtime connection.
const runtimeDrainTimeout = 2 * time.Second

// storedSession loads a Session with this operation's permissions and checks that
// it belongs to the selected Agent. It is private: callers request operations,
// never an arbitrary authorization verb followed by a separate action.
func (s *InteractionService) storedSession(ctx context.Context, verb auth.Verb, agent types.NamespacedName, contextID string) (*apiv1alpha1.Session, error) {
	session, err := s.sessions.getAuthorized(ctx, contextID, verb)
	if err != nil {
		return nil, interactionError(ctx, err)
	}
	if session.GetAgent().GetNamespace() != agent.Namespace || session.GetAgent().GetName() != agent.Name {
		return nil, a2atype.NewError(a2atype.ErrUnauthorized, "context does not belong to this Agent")
	}
	return session, nil
}

func (s *InteractionService) GetTask(ctx context.Context, agent types.NamespacedName, req *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	if req == nil {
		return nil, a2atype.ErrInvalidParams
	}
	session, err := s.taskSession(ctx, agent, auth.VerbGet, req.ID)
	if err != nil {
		return nil, err
	}
	task, err := s.store.GetSessionTask(ctx, session.GetId(), string(req.ID), req.HistoryLength)
	if errors.Is(err, database.ErrNotFound) {
		return nil, a2atype.ErrTaskNotFound
	}
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to load session task", "error", err, "task_id", req.ID)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load task")
	}
	return shapeTask(task, req.HistoryLength, true), nil
}

func (s *InteractionService) ListTasks(ctx context.Context, agent types.NamespacedName, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	if req == nil {
		req = &a2atype.ListTasksRequest{}
	}
	sessionIDs, err := s.listSessions(ctx, agent, req.ContextID)
	if err != nil {
		return nil, err
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "page size must be between 1 and 100")
	}

	afterID, err := decodeTaskPageToken(req.PageToken)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "invalid page token")
	}
	tasks, total, err := s.store.ListAgentTasks(ctx, sessionIDs, afterID, req.Status, req.StatusTimestampAfter, pageSize+1, req.HistoryLength)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to list session tasks", "error", err)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to list tasks")
	}
	response := &a2atype.ListTasksResponse{Tasks: tasks, TotalSize: total, PageSize: pageSize}
	if len(tasks) > pageSize {
		response.Tasks = tasks[:pageSize]
		response.NextPageToken = encodeTaskPageToken(string(response.Tasks[pageSize-1].ID))
	}
	for i, task := range response.Tasks {
		response.Tasks[i] = shapeTask(task, req.HistoryLength, req.IncludeArtifacts)
	}
	return response, nil
}

func (s *InteractionService) CancelTask(ctx context.Context, agent types.NamespacedName, req *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	if req == nil {
		return nil, a2atype.ErrInvalidParams
	}
	session, err := s.taskSession(ctx, agent, auth.VerbUpdate, req.ID)
	if err != nil {
		return nil, err
	}
	task, err := s.store.GetSessionTask(ctx, session.Id, string(req.ID), nil)
	if err != nil {
		return nil, s.storeError(ctx, err)
	}
	if task.Status.State.Terminal() {
		return task, nil
	}
	client, err := s.dial(ctx, session)
	if err != nil {
		return nil, err
	}
	closeRuntime := sync.OnceValue(client.Destroy)
	defer closeRuntime()
	result, err := client.CancelTask(ctx, req)
	if err != nil {
		_ = closeRuntime()
		if ctx.Err() == nil {
			if recovered, recoverErr := s.recoverBoundary(ctx, session.Id, req.ID, nil); recoverErr != nil || (recovered != nil && recovered.Status.State.Terminal()) {
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
		return s.awaitBoundary(ctx, session.Id, result.ID, nil)
	}
	return result, nil
}

func (s *InteractionService) SendMessage(ctx context.Context, agent types.NamespacedName, req *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	var historyLength *int
	if req != nil && req.Config != nil {
		historyLength = req.Config.HistoryLength
	}
	initialID := initialMessageID(req)
	session, err := s.prepareSend(ctx, agent, req)
	if err != nil {
		return nil, err
	}
	ctx, dispatchID, err := s.reserveDispatch(ctx, session.Id, initialID)
	if errors.Is(err, database.ErrMessageAccepted) {
		task, err := s.acceptedTask(ctx, session.Id, initialID, historyLength)
		if err != nil {
			return nil, err
		}
		if req.Config != nil && req.Config.ReturnImmediately {
			return task, nil
		}
		return s.awaitTaskCompletion(ctx, session.Id, task.ID, historyLength)
	}
	if err != nil {
		return nil, err
	}
	defer s.finishDispatch(ctx, session.Id, dispatchID, req.Message.ID, nil)
	client, err := s.dial(ctx, session)
	if err != nil {
		return nil, err
	}
	closeRuntime := sync.OnceValue(client.Destroy)
	defer closeRuntime()
	result, err := client.SendMessage(ctx, req)
	if err != nil {
		err = s.finishDispatch(ctx, session.Id, dispatchID, req.Message.ID, err)
		_ = closeRuntime()
		// Native settlement can pause the runtime before its unary response is
		// delivered. Recover only this accepted input's durable boundary.
		// The gRPC SDK maps proxy connection failures to A2A InternalError.
		// Preserve explicit protocol rejections, including unwrapped sentinels.
		if ctx.Err() == nil && a2atype.ErrorReason(err) == a2atype.ErrorReason(a2atype.ErrInternalError) {
			if task, readErr := s.store.GetSessionTaskByMessage(ctx, session.Id, string(req.Message.TaskID), req.Message.ID); readErr == nil {
				if recovered, recoverErr := s.recoverBoundary(ctx, session.Id, task.ID, historyLength); recovered != nil || recoverErr != nil {
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
		stored, err := s.awaitBoundary(ctx, session.Id, task.ID, historyLength)
		if err != nil {
			return nil, err
		}
		return stored, nil
	}
	return result, nil
}

func (s *InteractionService) SubscribeToTask(ctx context.Context, agent types.NamespacedName, req *a2atype.SubscribeToTaskRequest) iter.Seq2[a2atype.Event, error] {
	if req == nil {
		return errorEvents(a2atype.ErrInvalidParams)
	}
	session, err := s.taskSession(ctx, agent, auth.VerbGet, req.ID)
	if err != nil {
		return errorEvents(err)
	}
	task, err := s.store.GetSessionTask(ctx, session.Id, string(req.ID), nil)
	if err != nil {
		return errorEvents(s.storeError(ctx, err))
	}
	if isQuiescent(task.Status.State) {
		return func(yield func(a2atype.Event, error) bool) { yield(task, nil) }
	}
	client, err := s.dial(ctx, session)
	if err != nil {
		return errorEvents(err)
	}
	// The SDK subscription supplies its own initial task and later events.
	// Do not concatenate an unrelated stored snapshot with that live stream.
	return s.observe(ctx, session, req.ID, "", nil, client, client.SubscribeToTask(ctx, req))
}

func (s *InteractionService) SendStreamingMessage(ctx context.Context, agent types.NamespacedName, req *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	initialID := initialMessageID(req)
	session, err := s.prepareSend(ctx, agent, req)
	if err != nil {
		return errorEvents(err)
	}
	var historyLength *int
	if req.Config != nil {
		historyLength = req.Config.HistoryLength
	}
	return func(yield func(a2atype.Event, error) bool) {
		ctx, dispatchID, err := s.reserveDispatch(ctx, session.Id, initialID)
		if errors.Is(err, database.ErrMessageAccepted) {
			task, err := s.acceptedTask(ctx, session.Id, initialID, historyLength)
			if err != nil {
				yield(nil, err)
				return
			}
			if isQuiescent(task.Status.State) {
				yield(task, nil)
				return
			}
			s.SubscribeToTask(ctx, agent, &a2atype.SubscribeToTaskRequest{ID: task.ID})(yield)
			return
		}
		if err != nil {
			yield(nil, err)
			return
		}
		defer s.finishDispatch(ctx, session.Id, dispatchID, req.Message.ID, nil)
		client, err := s.dial(ctx, session)
		if err != nil {
			yield(nil, err)
			return
		}
		events := func(next func(a2atype.Event, error) bool) {
			for event, err := range client.SendStreamingMessage(ctx, req) {
				if err != nil {
					next(nil, s.finishDispatch(ctx, session.Id, dispatchID, req.Message.ID, err))
					return
				}
				if !next(event, nil) {
					return
				}
			}
			next(nil, s.finishDispatch(ctx, session.Id, dispatchID, req.Message.ID, a2atype.ErrInternalError))
		}
		s.observe(ctx, session, req.Message.TaskID, req.Message.ID, historyLength, client, events)(yield)
	}
}

// reserveDispatch waits only before forwarding input. No runtime send is retried
// on an ambiguous transport error, and no SQL lock is held during dispatch.
func (s *InteractionService) reserveDispatch(ctx context.Context, sessionID, initialID string) (context.Context, uuid.UUID, error) {
	id := uuid.New()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		err := s.store.ReserveSessionDispatch(ctx, sessionID, id, initialID)
		if err == nil {
			return a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{apia2a.DispatchHeader: {id.String()}}), id, nil
		}
		if errors.Is(err, database.ErrMessageAccepted) {
			return ctx, uuid.Nil, err
		}
		if !errors.Is(err, database.ErrDispatchBusy) {
			return ctx, uuid.Nil, s.storeError(ctx, err)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx, uuid.Nil, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return ctx, uuid.Nil, sendNotAccepted()
		case <-timer.C:
		}
	}
}

func sendNotAccepted() error {
	return a2atype.NewError(a2atype.ErrUnsupportedOperation, "input was not accepted; retry after the session becomes available").
		WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED", "retryAfterMs": "100"})
}

func (s *InteractionService) finishDispatch(ctx context.Context, sessionID string, id uuid.UUID, messageID string, sendErr error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	revoked, err := s.store.RevokeSessionDispatch(ctx, sessionID, id, messageID)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "release runtime dispatch", "session_id", sessionID, "error", err)
	} else if revoked && sendErr != nil && a2atype.ErrorReason(sendErr) == a2atype.ErrorReason(a2atype.ErrInternalError) {
		return sendNotAccepted()
	}
	return sendErr
}

// observe owns only this observer's runtime connection. Losing it cannot cancel
// execution. Final task state becomes visible after native cleanup acknowledges
// the saved version. Publication is independent of runtime pause/suspend.
func (s *InteractionService) observe(ctx context.Context, session *apiv1alpha1.Session, taskID a2atype.TaskID, messageID string, historyLength *int, client *a2aclient.Client, events iter.Seq2[a2atype.Event, error]) iter.Seq2[a2atype.Event, error] {
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
				task, err := s.awaitBoundary(ctx, session.Id, taskID, historyLength)
				yield(task, err)
				return
			}
			if !yield(event, nil) {
				return
			}
		}
		// Finish can race a subscription attach or stop the runtime stream.
		// Recover only durable public state; an incomplete stream is an error.
		if messageID != "" && ctx.Err() == nil {
			// A continuation's previous waiting boundary is not its response.
			task, err := s.store.GetSessionTaskByMessage(ctx, session.Id, string(taskID), messageID)
			if err == nil {
				taskID = task.ID
			} else {
				taskID = ""
			}
		}
		if taskID != "" && ctx.Err() == nil && (streamErr == nil || a2atype.ErrorReason(streamErr) == a2atype.ErrorReason(a2atype.ErrInternalError) || (messageID == "" && errors.Is(streamErr, a2atype.ErrTaskNotFound))) {
			_ = closeRuntime()
			if task, err := s.recoverBoundary(ctx, session.Id, taskID, historyLength); task != nil || err != nil {
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

// recoverBoundary reads a final result after its runtime connection was lost.
// A still-running task has no result to recover; it must not look completed.
func (s *InteractionService) recoverBoundary(ctx context.Context, sessionID string, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	task, err := s.store.GetSettledSessionTask(ctx, sessionID, string(taskID), historyLength)
	if errors.Is(err, database.ErrConflict) {
		return s.awaitBoundary(ctx, sessionID, taskID, historyLength)
	}
	if err == nil && isQuiescent(task.Status.State) {
		return task, nil
	}
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, s.storeError(ctx, err)
	}
	return nil, nil
}

func (s *InteractionService) dial(ctx context.Context, session *apiv1alpha1.Session) (*a2aclient.Client, error) {
	client, err := s.dialer.Dial(ctx, session)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "connect to runtime", "session_id", session.Id, "error", err)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to connect to Session runtime")
	}
	return client, nil
}

func (s *InteractionService) awaitBoundary(ctx context.Context, sessionID string, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		task, err := s.store.GetSettledSessionTask(ctx, sessionID, string(taskID), historyLength)
		if err == nil {
			return task, nil
		}
		if !errors.Is(err, database.ErrConflict) {
			return nil, s.storeError(ctx, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *InteractionService) GetAgentCard(ctx context.Context, ref types.NamespacedName) (*a2atype.AgentCard, error) {
	if err := validateInteractionAgent(ref); err != nil {
		return nil, err
	}
	var revisionID string
	if share, ok := auth.ShareContextFrom(ctx); ok {
		session, err := s.storedSession(ctx, auth.VerbGet, ref, share.SessionID)
		if err != nil {
			return nil, err
		}
		revisionID = session.PreparedRevision
	} else {
		agent, err := s.agents.Get(ctx, ref)
		if err != nil {
			return nil, interactionError(ctx, err)
		}
		revisionID = agent.Status.LatestSuccessfulRevision
	}
	if revisionID == "" {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "Agent has no successful revision")
	}
	revision, err := s.store.GetRuntimeRevision(ctx, revisionID)
	if err != nil {
		return nil, a2atype.ErrInternalError
	}
	card, err := apia2a.FromProtoAgentCard(revision.AgentCard)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to decode session agent card", "error", err, "revision", revision.Revision)
		return nil, a2atype.NewError(a2atype.ErrInternalError, "failed to load Agent Card")
	}

	return card, nil
}

func (s *InteractionService) prepareSend(ctx context.Context, agent types.NamespacedName, req *a2atype.SendMessageRequest) (*apiv1alpha1.Session, error) {
	if req == nil || req.Message == nil || req.Message.ID == "" {
		return nil, a2atype.ErrInvalidParams
	}
	if req.Config != nil && req.Config.PushConfig != nil {
		return nil, a2atype.ErrPushNotificationNotSupported
	}
	if err := validateInteractionAgent(agent); err != nil {
		return nil, err
	}
	apia2a.SanitizeCallerRequest(req)
	var session *apiv1alpha1.Session
	var err error
	switch {
	case req.Message.TaskID != "":
		session, err = s.taskSession(ctx, agent, auth.VerbUpdate, req.Message.TaskID)
	case req.Message.ContextID != "":
		session, err = s.storedSession(ctx, auth.VerbCreate, agent, req.Message.ContextID)
	default:
		if _, err = s.agents.Get(ctx, agent); err != nil {
			return nil, interactionError(ctx, err)
		}
		// Session creation already deduplicates request IDs per authenticated creator.
		requestID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("a2a/"+agent.Namespace+"/"+agent.Name+"/"+req.Message.ID)).String()
		session, err = s.sessions.Create(ctx, &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}, requestID, "")
		if err != nil {
			return nil, interactionError(ctx, err)
		}
	}
	if err != nil {
		return nil, err
	}
	if req.Message.ContextID != "" && req.Message.ContextID != session.ContextId {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message context does not match task")
	}
	req.Message.ContextID = session.ContextId
	if session.State != apiv1alpha1.SessionState_SESSION_STATE_READY || session.Operation != apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED {
		return nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "Session cannot accept work during a lifecycle operation")
	}
	return session, nil
}

func initialMessageID(req *a2atype.SendMessageRequest) string {
	if req != nil && req.Message != nil && req.Message.ContextID == "" && req.Message.TaskID == "" {
		return req.Message.ID
	}
	return ""
}

func requiresInput(state a2atype.TaskState) bool {
	return state == a2atype.TaskStateInputRequired || state == a2atype.TaskStateAuthRequired
}

func isQuiescent(state a2atype.TaskState) bool {
	return state.Terminal() || requiresInput(state)
}

func (s *InteractionService) storeError(ctx context.Context, err error) error {
	if errors.Is(err, database.ErrNotFound) {
		return a2atype.ErrTaskNotFound
	}
	if errors.Is(err, database.ErrFailedPrecondition) {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "reply does not match the pending input request")
	}
	if errors.Is(err, database.ErrIdempotencyConflict) {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "message ID was already used with a different request")
	}
	if errors.Is(err, database.ErrConflict) {
		return a2atype.NewError(a2atype.ErrUnsupportedOperation, err.Error())
	}
	logging.FromContext(ctx).ErrorContext(ctx, "read session task", "error", err)
	return a2atype.NewError(a2atype.ErrInternalError, "failed to read task")
}

func shapeTask(task *a2atype.Task, historyLength *int, includeArtifacts bool) *a2atype.Task {
	result := *task
	if historyLength != nil {
		switch {
		case *historyLength == 0:
			result.History = []*a2atype.Message{}
		case *historyLength > 0 && *historyLength < len(result.History):
			result.History = result.History[len(result.History)-*historyLength:]
		}
	}
	if !includeArtifacts {
		result.Artifacts = nil
	}
	return &result
}

func encodeTaskPageToken(taskID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(taskID))
}

func decodeTaskPageToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) == 0 {
		return "", fmt.Errorf("invalid page token")
	}
	return string(decoded), nil
}

func errorEvents(err error) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		var zero a2atype.Event
		yield(zero, err)
	}
}

// acceptedTask recovers a previously admitted first message without dispatching it again.
func (s *InteractionService) acceptedTask(ctx context.Context, sessionID, messageID string, historyLength *int) (*a2atype.Task, error) {
	task, err := s.store.GetSessionTaskByMessage(ctx, sessionID, "", messageID)
	if err != nil {
		return nil, s.storeError(ctx, err)
	}
	return shapeTask(task, historyLength, true), nil
}

func (s *InteractionService) awaitTaskCompletion(ctx context.Context, sessionID string, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	timer := time.NewTicker(50 * time.Millisecond)
	defer timer.Stop()
	for {
		task, err := s.awaitBoundary(ctx, sessionID, taskID, historyLength)
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

func (s *InteractionService) taskSession(ctx context.Context, agent types.NamespacedName, verb auth.Verb, taskID a2atype.TaskID) (*apiv1alpha1.Session, error) {
	if err := validateInteractionAgent(agent); err != nil {
		return nil, err
	}
	if _, ok := auth.AuthSessionFrom(ctx); !ok {
		return nil, a2atype.ErrUnauthenticated
	}
	if taskID == "" {
		return nil, a2atype.ErrInvalidParams
	}
	id, err := s.store.SessionForTask(ctx, string(taskID))
	if err != nil {
		return nil, s.storeError(ctx, err)
	}
	return s.storedSession(ctx, verb, agent, id)
}

func (s *InteractionService) listSessions(ctx context.Context, agent types.NamespacedName, contextID string) ([]string, error) {
	if err := validateInteractionAgent(agent); err != nil {
		return nil, err
	}
	if contextID != "" {
		session, err := s.storedSession(ctx, auth.VerbGet, agent, contextID)
		if err != nil {
			return nil, err
		}
		return []string{session.Id}, nil
	}
	ids := []string{}
	request := ListRequest{Agent: &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name}, PageSize: 100}
	for {
		page, err := s.sessions.List(ctx, request)
		if err != nil {
			return nil, interactionError(ctx, err)
		}
		for _, session := range page.Sessions {
			ids = append(ids, session.Id)
		}
		if page.NextPageToken == "" {
			return ids, nil
		}
		request.PageToken = page.NextPageToken
	}
}

// A missing Agent must never turn a task list into an unfiltered Session query.
// Direct service callers receive the same validation as transport callers.
func validateInteractionAgent(agent types.NamespacedName) error {
	if len(validation.IsDNS1123Label(agent.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(agent.Name)) != 0 {
		return a2atype.NewError(a2atype.ErrInvalidRequest, "Agent namespace and name are required and must be valid")
	}
	return nil
}

func interactionError(ctx context.Context, err error) error {
	switch serviceerrors.CodeOf(err) {
	case serviceerrors.CodeUnauthenticated:
		return a2atype.ErrUnauthenticated
	case serviceerrors.CodePermissionDenied, serviceerrors.CodeNotFound:
		return a2atype.ErrUnauthorized
	case serviceerrors.CodeInvalidArgument, serviceerrors.CodeAlreadyExists:
		return a2atype.NewError(a2atype.ErrInvalidRequest, serviceerrors.MessageOf(err))
	case serviceerrors.CodeFailedPrecondition, serviceerrors.CodeAborted:
		return a2atype.NewError(a2atype.ErrUnsupportedOperation, serviceerrors.MessageOf(err))
	default:
		logging.FromContext(ctx).ErrorContext(ctx, "agent conversation operation failed", "error", err)
		return a2atype.ErrInternalError
	}
}
