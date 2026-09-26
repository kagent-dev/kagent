package session

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
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

// InteractionService owns authorization, Session resolution, and persisted A2A
// state. It never connects to actors: the gateway owns dispatch and observation.
// These operations form the persistence boundary for a separate API service.
type InteractionService struct {
	store    interactionStore
	agents   agentService
	sessions *Service
}

func NewInteractionService(store interactionStore, agents agentService, sessions *Service) *InteractionService {
	return &InteractionService{store: store, agents: agents, sessions: sessions}
}

// PreparedSend either grants a new dispatch or returns an already accepted task.
// DispatchID is only set for a new attempt; the runtime must present it when saving
// the input. No database lock is held while the gateway contacts the actor.
type PreparedSend struct {
	Session      *apiv1alpha1.Session
	DispatchID   uuid.UUID
	AcceptedTask *a2atype.Task
}

// storedSession keeps operation permissions and Agent membership checks private.
// Callers request domain operations, never an arbitrary authorization verb.
func (s *InteractionService) storedSession(ctx context.Context, verb auth.Verb, agent types.NamespacedName, contextID string) (*apiv1alpha1.Session, error) {
	if err := validateInteractionAgent(agent); err != nil {
		return nil, err
	}
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

// PrepareSend authorizes and resolves the conversation, then reserves its input.
// An initial-message retry returns its saved task without granting another send.
func (s *InteractionService) PrepareSend(ctx context.Context, agent types.NamespacedName, req *a2atype.SendMessageRequest) (*PreparedSend, error) {
	initialID := initialMessageID(req)
	session, err := s.resolveSend(ctx, agent, req)
	if err != nil {
		return nil, err
	}
	id, err := s.reserveDispatch(ctx, session.Id, initialID)
	if errors.Is(err, database.ErrMessageAccepted) {
		var historyLength *int
		if req.Config != nil {
			historyLength = req.Config.HistoryLength
		}
		task, err := s.acceptedTask(ctx, session.Id, initialID, historyLength)
		if err != nil {
			return nil, err
		}
		return &PreparedSend{Session: session, AcceptedTask: task}, nil
	}
	if err != nil {
		return nil, err
	}
	return &PreparedSend{Session: session, DispatchID: id}, nil
}

// PrepareCancelTask authorizes cancellation and returns the actor target and
// committed task. A terminal task needs no actor call.
func (s *InteractionService) PrepareCancelTask(ctx context.Context, agent types.NamespacedName, req *a2atype.CancelTaskRequest) (*apiv1alpha1.Session, *a2atype.Task, error) {
	if req == nil {
		return nil, nil, a2atype.ErrInvalidParams
	}
	return s.prepareTask(ctx, agent, req.ID, auth.VerbUpdate)
}

// PrepareTaskSubscription resolves a read-authorized task. The gateway decides
// whether to return this snapshot or subscribe to the actor's live stream.
func (s *InteractionService) PrepareTaskSubscription(ctx context.Context, agent types.NamespacedName, req *a2atype.SubscribeToTaskRequest) (*apiv1alpha1.Session, *a2atype.Task, error) {
	if req == nil {
		return nil, nil, a2atype.ErrInvalidParams
	}
	return s.prepareTask(ctx, agent, req.ID, auth.VerbGet)
}

func (s *InteractionService) prepareTask(ctx context.Context, agent types.NamespacedName, id a2atype.TaskID, verb auth.Verb) (*apiv1alpha1.Session, *a2atype.Task, error) {
	session, err := s.taskSession(ctx, agent, verb, id)
	if err != nil {
		return nil, nil, err
	}
	task, err := s.store.GetSessionTask(ctx, session.Id, string(id), nil)
	if err != nil {
		return nil, nil, interactionStoreError(ctx, err)
	}
	return session, task, nil
}

// reserveDispatch waits only before forwarding input. No runtime send is retried
// on an ambiguous transport error, and no SQL lock is held during dispatch.
func (s *InteractionService) reserveDispatch(ctx context.Context, sessionID, initialID string) (uuid.UUID, error) {
	id := uuid.New()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		err := s.store.ReserveSessionDispatch(ctx, sessionID, id, initialID)
		if err == nil {
			return id, nil
		}
		if errors.Is(err, database.ErrMessageAccepted) {
			return uuid.Nil, err
		}
		if !errors.Is(err, database.ErrDispatchBusy) {
			return uuid.Nil, interactionStoreError(ctx, err)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return uuid.Nil, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			return uuid.Nil, ErrSendNotAccepted
		case <-timer.C:
		}
	}
}

// ErrSendNotAccepted tells callers they may retry this input because no dispatch
// occurred, or an unused dispatch was revoked before the runtime persisted it.
var ErrSendNotAccepted = a2atype.NewError(a2atype.ErrUnsupportedOperation, "input was not accepted; retry after the session becomes available").
	WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED", "retryAfterMs": "100"})

// RevokeSend releases an unused attempt. A true result proves that the input was
// not accepted, allowing the gateway to distinguish rejection from uncertainty.
func (s *InteractionService) RevokeSend(ctx context.Context, agent types.NamespacedName, message *a2atype.Message, id uuid.UUID) (bool, error) {
	session, err := s.messageSession(ctx, agent, message)
	if err != nil {
		return false, err
	}
	revoked, err := s.store.RevokeSessionDispatch(ctx, session.Id, id, message.ID)
	if err != nil {
		return false, interactionStoreError(ctx, err)
	}
	return revoked, nil
}

// GetTaskByMessage reads only the task that accepted this input. It uses the
// send's permissions and Session scope, so recovery cannot expose another task.
func (s *InteractionService) GetTaskByMessage(ctx context.Context, agent types.NamespacedName, message *a2atype.Message) (*a2atype.Task, error) {
	session, err := s.messageSession(ctx, agent, message)
	if err != nil {
		return nil, err
	}
	task, err := s.store.GetSessionTaskByMessage(ctx, session.Id, string(message.TaskID), message.ID)
	if err != nil {
		return nil, interactionStoreError(ctx, err)
	}
	return task, nil
}

// GetSettledTask waits for an already staged save to be published after native
// cleanup. It only reads persistence; it cannot wake or contact the actor.
func (s *InteractionService) GetSettledTask(ctx context.Context, agent types.NamespacedName, sessionID string, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	session, err := s.storedSession(ctx, auth.VerbGet, agent, sessionID)
	if err != nil {
		return nil, err
	}
	return s.awaitBoundary(ctx, session.Id, taskID, historyLength)
}

// GetSendResult reads the committed response using the original send's permission.
// A create-only caller does not need an additional read or update grant merely
// because the gateway must recover its response from storage.
func (s *InteractionService) GetSendResult(ctx context.Context, agent types.NamespacedName, message *a2atype.Message, taskID a2atype.TaskID, historyLength *int) (*a2atype.Task, error) {
	session, err := s.messageSession(ctx, agent, message)
	if err != nil {
		return nil, err
	}
	if message.TaskID != "" && message.TaskID != taskID {
		return nil, a2atype.ErrInvalidParams
	}
	return s.awaitBoundary(ctx, session.Id, taskID, historyLength)
}

// GetCancelResult reads a cancellation's committed result using cancel authority.
func (s *InteractionService) GetCancelResult(ctx context.Context, agent types.NamespacedName, taskID a2atype.TaskID) (*a2atype.Task, error) {
	session, err := s.taskSession(ctx, agent, auth.VerbUpdate, taskID)
	if err != nil {
		return nil, err
	}
	return s.awaitBoundary(ctx, session.Id, taskID, nil)
}

// messageSession rechecks send authority without creating a Session or requiring
// runtime readiness. Cleanup and recovery must also work after lifecycle changes.
func (s *InteractionService) messageSession(ctx context.Context, agent types.NamespacedName, message *a2atype.Message) (*apiv1alpha1.Session, error) {
	if message == nil || message.ID == "" {
		return nil, a2atype.ErrInvalidParams
	}
	if err := validateInteractionAgent(agent); err != nil {
		return nil, err
	}
	var session *apiv1alpha1.Session
	var err error
	if message.TaskID != "" {
		session, err = s.taskSession(ctx, agent, auth.VerbUpdate, message.TaskID)
	} else {
		session, err = s.storedSession(ctx, auth.VerbCreate, agent, message.ContextID)
	}
	if err != nil {
		return nil, err
	}
	if message.ContextID != "" && message.ContextID != session.ContextId {
		return nil, a2atype.NewError(a2atype.ErrInvalidRequest, "message context does not match task")
	}
	return session, nil
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
			return nil, interactionStoreError(ctx, err)
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

func (s *InteractionService) resolveSend(ctx context.Context, agent types.NamespacedName, req *a2atype.SendMessageRequest) (*apiv1alpha1.Session, error) {
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
	case req.Message.TaskID != "" || req.Message.ContextID != "":
		session, err = s.messageSession(ctx, agent, req.Message)
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

func interactionStoreError(ctx context.Context, err error) error {
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

// acceptedTask recovers an admitted first message without dispatching it again.
func (s *InteractionService) acceptedTask(ctx context.Context, sessionID, messageID string, historyLength *int) (*a2atype.Task, error) {
	task, err := s.store.GetSessionTaskByMessage(ctx, sessionID, "", messageID)
	if err != nil {
		return nil, interactionStoreError(ctx, err)
	}
	return shapeTask(task, historyLength, true), nil
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
		return nil, interactionStoreError(ctx, err)
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

func initialMessageID(req *a2atype.SendMessageRequest) string {
	if req != nil && req.Message != nil && req.Message.ContextID == "" && req.Message.TaskID == "" {
		return req.Message.ID
	}
	return ""
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
