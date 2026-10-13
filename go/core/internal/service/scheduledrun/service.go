package scheduledrun

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	schedules "github.com/kagent-dev/kagent/go/api/scheduledrun"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/protobuf/proto"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type serviceStore interface {
	FindScheduledRunRequest(context.Context, string, string, []byte) (*apiv1alpha1.ScheduledRun, error)
	CreateScheduledRun(context.Context, *apiv1alpha1.ScheduledRun, string, []byte) (*apiv1alpha1.ScheduledRun, error)
	GetScheduledRun(context.Context, uuid.UUID, string) (*apiv1alpha1.ScheduledRun, error)
	ListScheduledRuns(context.Context, database.ScheduledRunQuery) ([]*apiv1alpha1.ScheduledRun, error)
	UpdateScheduledRun(context.Context, uuid.UUID, string, string, *apiv1alpha1.ScheduledRunConfig) (*apiv1alpha1.ScheduledRun, error)
	DeleteScheduledRun(context.Context, uuid.UUID, string) (*apiv1alpha1.ScheduledRun, error)
	TriggerScheduledRun(context.Context, uuid.UUID, string, string) (*apiv1alpha1.ScheduledRunExecution, error)
	GetScheduledRunExecution(context.Context, uuid.UUID, string) (*apiv1alpha1.ScheduledRunExecution, error)
	ListScheduledRunExecutions(context.Context, database.ScheduledRunExecutionQuery) ([]*apiv1alpha1.ScheduledRunExecution, error)
}

type Service struct {
	store      serviceStore
	kube       client.Reader
	authorizer auth.Authorizer
}

type CreateRequest struct {
	Agent     *apiv1alpha1.ResourceReference
	RequestID string
	Config    *apiv1alpha1.ScheduledRunConfig
}

func NewService(store serviceStore, kube client.Reader, authorizer auth.Authorizer) *Service {
	return &Service{store: store, kube: kube, authorizer: authorizer}
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (*apiv1alpha1.ScheduledRun, error) {
	creator, err := s.authorize(ctx, auth.VerbCreate, "")
	if err != nil {
		return nil, err
	}
	config, err := normalizeConfig(request.Config)
	if err != nil {
		return nil, err
	}
	// Hash original normalized inputs, not the mutable persisted configuration.
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&apiv1alpha1.CreateScheduledRunRequest{
		Agent: request.Agent, Config: config,
	})
	if err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid schedule inputs", err)
	}
	hash := sha256.Sum256(data)
	existing, err := s.store.FindScheduledRunRequest(ctx, creator, request.RequestID, hash[:])
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return nil, storeError(err)
	}
	schedule := &apiv1alpha1.ScheduledRun{
		Creator: creator, Config: config,
		Agent: proto.CloneOf(request.Agent),
	}
	if err := s.authorizeTarget(ctx, schedule); err != nil {
		return nil, err
	}
	if err := s.validateTarget(ctx, schedule); err != nil {
		return nil, err
	}
	result, err := s.store.CreateScheduledRun(ctx, schedule, request.RequestID, hash[:])
	return result, storeError(err)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*apiv1alpha1.ScheduledRun, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, id.String())
	if err != nil {
		return nil, err
	}
	result, err := s.store.GetScheduledRun(ctx, id, creator)
	return result, storeError(err)
}

func (s *Service) List(ctx context.Context, page *apiv1alpha1.PageRequest) ([]*apiv1alpha1.ScheduledRun, string, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, "")
	if err != nil {
		return nil, "", err
	}
	query, err := listQuery(page, creator)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.store.ListScheduledRuns(ctx, query)
	if err != nil {
		return nil, "", storeError(err)
	}
	if len(rows) == query.Limit {
		rows = rows[:len(rows)-1]
		return rows, pageToken(rows[len(rows)-1].Id), nil
	}
	return rows, "", nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, etag string, config *apiv1alpha1.ScheduledRunConfig) (*apiv1alpha1.ScheduledRun, error) {
	creator, err := s.authorize(ctx, auth.VerbUpdate, id.String())
	if err != nil {
		return nil, err
	}
	config, err = normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.GetScheduledRun(ctx, id, creator)
	if err != nil {
		return nil, storeError(err)
	}
	if err := s.authorizeTarget(ctx, existing); err != nil {
		return nil, err
	}
	// Target readiness is checked by execution. A missing target must not prevent pausing.
	result, err := s.store.UpdateScheduledRun(ctx, id, creator, etag, config)
	return result, storeError(err)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) (*apiv1alpha1.ScheduledRun, error) {
	creator, err := s.authorize(ctx, auth.VerbDelete, id.String())
	if err != nil {
		return nil, err
	}
	result, err := s.store.DeleteScheduledRun(ctx, id, creator)
	return result, storeError(err)
}

func (s *Service) Trigger(ctx context.Context, id uuid.UUID, requestID string) (*apiv1alpha1.ScheduledRunExecution, error) {
	creator, err := s.authorize(ctx, auth.VerbCreate, id.String()+"/executions")
	if err != nil {
		return nil, err
	}
	existing, err := s.store.GetScheduledRun(ctx, id, creator)
	if err != nil {
		return nil, storeError(err)
	}
	if err := s.authorizeTarget(ctx, existing); err != nil {
		return nil, err
	}
	result, err := s.store.TriggerScheduledRun(ctx, id, creator, requestID)
	return result, storeError(err)
}

func (s *Service) GetExecution(ctx context.Context, id uuid.UUID) (*apiv1alpha1.ScheduledRunExecution, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, "executions/"+id.String())
	if err != nil {
		return nil, err
	}
	result, err := s.store.GetScheduledRunExecution(ctx, id, creator)
	return result, storeError(err)
}

func (s *Service) ListExecutions(ctx context.Context, id uuid.UUID, page *apiv1alpha1.PageRequest) ([]*apiv1alpha1.ScheduledRunExecution, string, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, id.String()+"/executions")
	if err != nil {
		return nil, "", err
	}
	query, err := listQuery(page, creator)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.store.ListScheduledRunExecutions(ctx, database.ScheduledRunExecutionQuery{ScheduledRunQuery: query, ScheduledRunID: id})
	if err != nil {
		return nil, "", storeError(err)
	}
	if len(rows) == query.Limit {
		rows = rows[:len(rows)-1]
		return rows, pageToken(rows[len(rows)-1].Id), nil
	}
	return rows, "", nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, name string) (string, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session.Principal().User.ID == "" {
		return "", serviceerrors.NewUnauthenticated("Authentication is required", nil)
	}
	// A session capability never grants schedule access, even for in-process callers.
	if _, shared := auth.ShareContextFrom(ctx); shared {
		return "", serviceerrors.NewPermissionDenied("Session shares do not grant schedule access", nil)
	}
	principal := session.Principal()
	if err := s.authorizer.Check(ctx, principal, verb, auth.Resource{Type: "ScheduledRun", Name: name}); err != nil {
		return "", serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return principal.User.ID, nil
}

func (s *Service) authorizeTarget(ctx context.Context, schedule *apiv1alpha1.ScheduledRun) error {
	session, _ := auth.AuthSessionFrom(ctx) // Each public operation authorizes before target access.
	principal := session.Principal()
	if err := s.authorizer.Check(ctx, principal, auth.VerbGet, auth.Resource{
		Type: "Agent", Namespace: schedule.Agent.Namespace, Name: schedule.Agent.Name,
	}); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized to run Agent", err)
	}
	if err := s.authorizer.Check(ctx, principal, auth.VerbCreate, auth.Resource{Type: "Session"}); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized to create Session", err)
	}
	return nil
}

func (s *Service) validateTarget(ctx context.Context, schedule *apiv1alpha1.ScheduledRun) error {
	agent := &v1alpha3.Agent{}
	if err := s.kube.Get(ctx, types.NamespacedName{Namespace: schedule.Agent.Namespace, Name: schedule.Agent.Name}, agent); err != nil {
		if apierrors.IsNotFound(err) {
			return serviceerrors.NewFailedPrecondition("Agent does not exist", err)
		}
		return serviceerrors.NewInternal("Failed to resolve Agent", err)
	}
	return nil
}

func normalizeConfig(config *apiv1alpha1.ScheduledRunConfig) (*apiv1alpha1.ScheduledRunConfig, error) {
	config = schedules.Normalize(config)
	if _, err := schedules.Next(config, time.Now()); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid schedule", err)
	}
	return config, nil
}

func listQuery(page *apiv1alpha1.PageRequest, creator string) (database.ScheduledRunQuery, error) {
	limit := int(page.GetLimit())
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return database.ScheduledRunQuery{}, serviceerrors.NewInvalidArgument("Invalid page size", nil)
	}
	var after *uuid.UUID
	if page.GetPageToken() != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(page.GetPageToken())
		if err != nil {
			return database.ScheduledRunQuery{}, serviceerrors.NewInvalidArgument("Invalid page token", err)
		}
		id, err := uuid.Parse(string(decoded))
		if err != nil {
			return database.ScheduledRunQuery{}, serviceerrors.NewInvalidArgument("Invalid page token", err)
		}
		after = &id
	}
	return database.ScheduledRunQuery{Creator: creator, AfterID: after, Limit: limit + 1}, nil
}

func pageToken(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }

func storeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, database.ErrNotFound):
		return serviceerrors.NewNotFound("Scheduled run not found", err)
	case errors.Is(err, database.ErrIdempotencyConflict):
		return serviceerrors.NewAlreadyExists("Request ID was already used for different inputs", err)
	case errors.Is(err, database.ErrConflict):
		return serviceerrors.NewAborted(err.Error(), err)
	case errors.Is(err, database.ErrFailedPrecondition):
		return serviceerrors.NewFailedPrecondition(err.Error(), err)
	default:
		return serviceerrors.NewInternal("Scheduled run operation failed", err)
	}
}
