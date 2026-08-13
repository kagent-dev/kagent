package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type Store interface {
	StoreSession(context.Context, *database.Session) error
	GetSession(context.Context, string, string) (*database.Session, error)
	ListSessions(context.Context, string) ([]database.Session, error)
	ListSessionsForAgent(context.Context, string, string) ([]database.SessionWithShareToken, error)
	ListSessionsForAgentAllUsers(context.Context, string) ([]database.Session, error)
	DeleteSession(context.Context, string, string) error
	GetAgent(context.Context, string) (*database.Agent, error)
	StoreEvents(context.Context, ...*database.Event) error
	ListEventsForSession(context.Context, string, string, database.QueryOptions) ([]*database.Event, error)
	CreateSessionShare(context.Context, *database.SessionShare) (*database.SessionShare, error)
	ListSessionSharesBySession(context.Context, string) ([]database.SessionShare, error)
	DeleteSessionShare(context.Context, string, string, string) error
}

type SandboxActorCleaner interface {
	DeleteSandboxAgentSessionActor(context.Context, *v1alpha3.SandboxAgent, string) (bool, error)
}

type Service struct {
	store        Store
	kube         client.Client
	actorCleaner SandboxActorCleaner
	token        func() (string, error)
}

type Option func(*Service)

type CreateRequest struct {
	ID       *string
	AgentRef string
	Name     *string
	Source   *database.SessionSource
}

type UpdateRequest struct {
	SessionID string
	Name      *string
	AgentRef  *string
}

type AddEventRequest struct {
	SessionID string
	ID        string
	Data      string
}

type GetResult struct {
	Session  *database.Session
	Events   []*database.Event
	ReadOnly *bool
}

func NewService(store Store, options ...Option) *Service {
	service := &Service{store: store, token: generateShareToken}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithSandboxLifecycle(kube client.Client, cleaner SandboxActorCleaner) Option {
	return func(service *Service) {
		service.kube = kube
		service.actorCleaner = cleaner
	}
}

func WithShareTokenGenerator(generator func() (string, error)) Option {
	return func(service *Service) {
		if generator != nil {
			service.token = generator
		}
	}
}

func (s *Service) List(ctx context.Context) ([]database.Session, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to list sessions", fmt.Errorf("database client is not configured"))
	}
	sessions, err := s.store.ListSessions(ctx, userID)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to list sessions", err)
	}
	return sessions, nil
}

func (s *Service) ListByAgent(ctx context.Context, namespace, name string) ([]database.SessionWithShareToken, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if namespace == "" || name == "" {
		return nil, serviceerrors.NewInvalidArgument("Agent namespace and name are required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to get sessions for agent", fmt.Errorf("database client is not configured"))
	}

	agentID := utils.ConvertToPythonIdentifier(namespace + "/" + name)
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, mapStoreError("Agent not found", err)
	}
	sessions, err := s.store.ListSessionsForAgent(ctx, agentID, userID)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to get sessions for agent", err)
	}
	return sessions, nil
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (*database.Session, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.AgentRef) == "" {
		return nil, serviceerrors.NewInvalidArgument("agent_ref is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to create session", fmt.Errorf("database client is not configured"))
	}

	id := string(a2a.NewContextID())
	if request.ID != nil && *request.ID != "" {
		id = *request.ID
	}
	agentID := utils.ConvertToPythonIdentifier(request.AgentRef)
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, serviceerrors.NewInvalidArgument(fmt.Sprintf("Agent ref is invalid, please check the agent ref %s", request.AgentRef), err)
	}
	if agent.WorkloadType == v1alpha3.WorkloadModeSandbox {
		_, isSubstrateSandbox, err := s.lookupSubstrateSandboxAgent(ctx, request.AgentRef)
		if err != nil {
			return nil, serviceerrors.NewInternal("Failed to inspect sandbox agent", err)
		}
		if !isSubstrateSandbox {
			existing, err := s.store.ListSessionsForAgentAllUsers(ctx, agentID)
			if err != nil {
				return nil, serviceerrors.NewInternal("Failed to list sessions for agent", err)
			}
			if len(existing) > 0 {
				return nil, serviceerrors.NewAlreadyExists("Sandbox agents support only one chat session", fmt.Errorf("a session already exists for this agent"))
			}
		}
	}

	value := &database.Session{
		ID:      id,
		Name:    request.Name,
		UserID:  userID,
		AgentID: &agentID,
		Source:  request.Source,
	}
	if err := s.store.StoreSession(ctx, value); err != nil {
		if errors.Is(err, database.ErrSessionIDInUse) {
			return nil, serviceerrors.NewAlreadyExists("Session ID is already in use", err)
		}
		return nil, serviceerrors.NewInternal("Failed to create session", err)
	}
	stored, err := s.store.GetSession(ctx, id, userID)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to load created session", err)
	}
	return stored, nil
}

func (s *Service) Get(ctx context.Context, sessionID string, options database.QueryOptions) (GetResult, error) {
	if strings.TrimSpace(sessionID) == "" {
		return GetResult{}, serviceerrors.NewInvalidArgument("session_id is required", nil)
	}
	userID, err := effectiveUserID(ctx, sessionID)
	if err != nil {
		return GetResult{}, err
	}
	if s.store == nil {
		return GetResult{}, serviceerrors.NewInternal("Failed to get session", fmt.Errorf("database client is not configured"))
	}
	session, err := s.store.GetSession(ctx, sessionID, userID)
	if err != nil {
		return GetResult{}, mapStoreError("Session not found", err)
	}
	events, err := s.store.ListEventsForSession(ctx, sessionID, userID, options)
	if err != nil {
		return GetResult{}, serviceerrors.NewInternal("Failed to get events for session", err)
	}

	result := GetResult{Session: session, Events: events}
	if share, ok := auth.ShareContextFrom(ctx); ok && share.SessionID == sessionID && share.ReadOnly {
		readOnly := true
		result.ReadOnly = &readOnly
	}
	return result, nil
}

func (s *Service) Update(ctx context.Context, request UpdateRequest) (*database.Session, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.SessionID) == "" {
		return nil, serviceerrors.NewInvalidArgument("session_id is required", nil)
	}
	if request.Name == nil && request.AgentRef == nil {
		return nil, serviceerrors.NewInvalidArgument("at least one of name or agent_ref is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to update session", fmt.Errorf("database client is not configured"))
	}

	session, err := s.store.GetSession(ctx, request.SessionID, userID)
	if err != nil {
		return nil, mapStoreError("Session not found", err)
	}
	if request.Name != nil {
		session.Name = request.Name
	}
	if request.AgentRef != nil {
		agent, err := s.store.GetAgent(ctx, utils.ConvertToPythonIdentifier(*request.AgentRef))
		if err != nil {
			return nil, mapStoreError("Agent not found", err)
		}
		session.AgentID = &agent.ID
	}
	if err := s.store.StoreSession(ctx, session); err != nil {
		return nil, serviceerrors.NewInternal("Failed to update session", err)
	}
	return session, nil
}

func (s *Service) Delete(ctx context.Context, sessionID string) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" {
		return serviceerrors.NewInvalidArgument("session_id is required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("Failed to delete session", fmt.Errorf("database client is not configured"))
	}

	var cleanup *v1alpha3.SandboxAgent
	if s.actorCleaner != nil {
		if session, getErr := s.store.GetSession(ctx, sessionID, userID); getErr == nil && session != nil && session.AgentID != nil {
			if sandboxAgent, lookupErr := s.substrateSandboxAgentForSession(ctx, session); lookupErr == nil {
				cleanup = sandboxAgent
			}
		}
	}
	if err := s.store.DeleteSession(ctx, sessionID, userID); err != nil {
		return serviceerrors.NewInternal("Failed to delete session", err)
	}
	if cleanup != nil {
		if _, err := s.actorCleaner.DeleteSandboxAgentSessionActor(ctx, cleanup, sessionID); err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to delete substrate session actor", "sessionID", sessionID)
		}
	}
	return nil
}

func (s *Service) AddEvent(ctx context.Context, request AddEventRequest) (*database.Event, error) {
	if strings.TrimSpace(request.SessionID) == "" {
		return nil, serviceerrors.NewInvalidArgument("session_id is required", nil)
	}
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.Data) == "" {
		return nil, serviceerrors.NewInvalidArgument("event id and data are required", nil)
	}
	principal, err := authenticatedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	userID, err := effectiveUserID(ctx, request.SessionID)
	if err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("Failed to store event", fmt.Errorf("database client is not configured"))
	}
	session, err := s.store.GetSession(ctx, request.SessionID, userID)
	if err != nil {
		return nil, mapStoreError("Session not found", err)
	}
	if session.AgentID != nil && *session.AgentID != utils.ConvertToPythonIdentifier(principal.Agent.ID) {
		return nil, serviceerrors.NewPermissionDenied("Session does not belong to this agent", nil)
	}
	event := &database.Event{
		ID:        request.ID,
		SessionID: request.SessionID,
		Data:      request.Data,
		UserID:    userID,
	}
	if err := s.store.StoreEvents(ctx, event); err != nil {
		return nil, serviceerrors.NewInternal("Failed to store event", err)
	}
	return event, nil
}

func (s *Service) CreateShare(ctx context.Context, sessionID string, readOnly *bool) (*database.SessionShare, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, serviceerrors.NewInvalidArgument("session_id is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("failed to create share", fmt.Errorf("database client is not configured"))
	}
	if _, err := s.store.GetSession(ctx, sessionID, userID); err != nil {
		return nil, mapStoreError("session not found", err)
	}
	token, err := s.token()
	if err != nil {
		return nil, serviceerrors.NewInternal("failed to generate token", err)
	}
	isReadOnly := true
	if readOnly != nil {
		isReadOnly = *readOnly
	}
	created, err := s.store.CreateSessionShare(ctx, &database.SessionShare{
		Token:     token,
		SessionID: sessionID,
		UserID:    userID,
		ReadOnly:  isReadOnly,
	})
	if err != nil {
		return nil, serviceerrors.NewInternal("failed to create share", err)
	}
	return created, nil
}

func (s *Service) ListShares(ctx context.Context, sessionID string) ([]database.SessionShare, error) {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, serviceerrors.NewInvalidArgument("session_id is required", nil)
	}
	if s.store == nil {
		return nil, serviceerrors.NewInternal("failed to list shares", fmt.Errorf("database client is not configured"))
	}
	if _, err := s.store.GetSession(ctx, sessionID, userID); err != nil {
		return nil, mapStoreError("session not found", err)
	}
	shares, err := s.store.ListSessionSharesBySession(ctx, sessionID)
	if err != nil {
		return nil, serviceerrors.NewInternal("failed to list shares", err)
	}
	return shares, nil
}

func (s *Service) DeleteShare(ctx context.Context, sessionID, token string) error {
	userID, err := authenticatedUserID(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(token) == "" {
		return serviceerrors.NewInvalidArgument("session_id and token are required", nil)
	}
	if s.store == nil {
		return serviceerrors.NewInternal("failed to delete share", fmt.Errorf("database client is not configured"))
	}
	if _, err := s.store.GetSession(ctx, sessionID, userID); err != nil {
		return mapStoreError("session not found", err)
	}
	if err := s.store.DeleteSessionShare(ctx, token, sessionID, userID); err != nil {
		return serviceerrors.NewInternal("failed to delete share", err)
	}
	return nil
}

func (s *Service) lookupSubstrateSandboxAgent(ctx context.Context, agentRef string) (*v1alpha3.SandboxAgent, bool, error) {
	if s.kube == nil {
		return nil, false, nil
	}
	ref := strings.TrimSpace(agentRef)
	if ref == "" {
		return nil, false, nil
	}
	kubernetesRef := utils.ConvertToKubernetesIdentifier(ref)
	namespacedName, err := utils.ParseRefString(kubernetesRef, "")
	if err != nil {
		return nil, false, nil
	}
	sandboxAgent := &v1alpha3.SandboxAgent{}
	if err := s.kube.Get(ctx, namespacedName, sandboxAgent); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return sandboxAgent, true, nil
}

func (s *Service) substrateSandboxAgentForSession(ctx context.Context, session *database.Session) (*v1alpha3.SandboxAgent, error) {
	if session == nil || session.AgentID == nil {
		return nil, nil
	}
	agent, err := s.store.GetAgent(ctx, *session.AgentID)
	if err != nil {
		return nil, err
	}
	if agent.WorkloadType != v1alpha3.WorkloadModeSandbox {
		return nil, nil
	}
	sandboxAgent, isSubstrate, err := s.lookupSubstrateSandboxAgent(ctx, *session.AgentID)
	if err != nil || !isSubstrate {
		return nil, err
	}
	return sandboxAgent, nil
}

func authenticatedPrincipal(ctx context.Context) (auth.Principal, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return auth.Principal{}, serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	principal := session.Principal()
	if principal.User.ID == "" {
		return auth.Principal{}, serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("user id is empty"))
	}
	return principal, nil
}

func authenticatedUserID(ctx context.Context) (string, error) {
	principal, err := authenticatedPrincipal(ctx)
	if err != nil {
		return "", err
	}
	return principal.User.ID, nil
}

func effectiveUserID(ctx context.Context, sessionID string) (string, error) {
	if share, ok := auth.ShareContextFrom(ctx); ok && share.SessionID == sessionID {
		return share.UserID, nil
	}
	return authenticatedUserID(ctx)
}

func generateShareToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func mapStoreError(message string, err error) error {
	if errors.Is(err, database.ErrNotFound) {
		return serviceerrors.NewNotFound(message, err)
	}
	return serviceerrors.NewInternal(message, err)
}
