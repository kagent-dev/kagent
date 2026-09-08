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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type serviceStore interface {
	FindScheduledRunRequest(context.Context, string, string, []byte) (*apiv1alpha1.ScheduledRun, error)
	CreateScheduledRun(context.Context, *apiv1alpha1.ScheduledRun, string, []byte) (*apiv1alpha1.ScheduledRun, error)
	GetScheduledRun(context.Context, string, string) (*apiv1alpha1.ScheduledRun, error)
	ListScheduledRuns(context.Context, database.ScheduledRunQuery) ([]*apiv1alpha1.ScheduledRun, error)
	UpdateScheduledRun(context.Context, string, string, string, *apiv1alpha1.ScheduledRunConfig) (*apiv1alpha1.ScheduledRun, error)
	DeleteScheduledRun(context.Context, string, string) (*apiv1alpha1.ScheduledRun, error)
	TriggerScheduledRun(context.Context, string, string, string) (*apiv1alpha1.ScheduledRunExecution, error)
	GetScheduledRunExecution(context.Context, string, string) (*apiv1alpha1.ScheduledRunExecution, error)
	ListScheduledRunExecutions(context.Context, database.ScheduledRunExecutionQuery) ([]*apiv1alpha1.ScheduledRunExecution, error)
}

type Service struct {
	store      serviceStore
	kube       client.Reader
	authorizer auth.Authorizer
}

type CreateRequest struct {
	Harness, AgentTemplate *apiv1alpha1.ResourceReference
	RequestID              string
	Config                 *apiv1alpha1.ScheduledRunConfig
}

type ListRequest struct {
	PageToken string
	PageSize  int
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
		Harness: request.Harness, AgentTemplate: request.AgentTemplate, Config: config,
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
		Harness:       proto.CloneOf(request.Harness),
		AgentTemplate: proto.CloneOf(request.AgentTemplate),
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

func (s *Service) Get(ctx context.Context, id string) (*apiv1alpha1.ScheduledRun, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, id)
	if err != nil {
		return nil, err
	}
	result, err := s.store.GetScheduledRun(ctx, id, creator)
	return result, storeError(err)
}

func (s *Service) List(ctx context.Context, request ListRequest) ([]*apiv1alpha1.ScheduledRun, string, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, "")
	if err != nil {
		return nil, "", err
	}
	query, err := listQuery(request, creator)
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

func (s *Service) Update(ctx context.Context, id, etag string, config *apiv1alpha1.ScheduledRunConfig) (*apiv1alpha1.ScheduledRun, error) {
	creator, err := s.authorize(ctx, auth.VerbUpdate, id)
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

func (s *Service) Delete(ctx context.Context, id string) (*apiv1alpha1.ScheduledRun, error) {
	creator, err := s.authorize(ctx, auth.VerbDelete, id)
	if err != nil {
		return nil, err
	}
	result, err := s.store.DeleteScheduledRun(ctx, id, creator)
	return result, storeError(err)
}

func (s *Service) Trigger(ctx context.Context, id, requestID string) (*apiv1alpha1.ScheduledRunExecution, error) {
	creator, err := s.authorize(ctx, auth.VerbCreate, id+"/executions")
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

type ListExecutionsRequest struct {
	ListRequest
	ScheduledRunID string
}

func (s *Service) GetExecution(ctx context.Context, id string) (*apiv1alpha1.ScheduledRunExecution, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, "executions/"+id)
	if err != nil {
		return nil, err
	}
	result, err := s.store.GetScheduledRunExecution(ctx, id, creator)
	return result, storeError(err)
}

func (s *Service) ListExecutions(ctx context.Context, request ListExecutionsRequest) ([]*apiv1alpha1.ScheduledRunExecution, string, error) {
	creator, err := s.authorize(ctx, auth.VerbGet, request.ScheduledRunID+"/executions")
	if err != nil {
		return nil, "", err
	}
	query, err := listQuery(request.ListRequest, creator)
	if err != nil {
		return nil, "", err
	}
	rows, err := s.store.ListScheduledRunExecutions(ctx, database.ScheduledRunExecutionQuery{ScheduledRunQuery: query, ScheduledRunID: request.ScheduledRunID})
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
	// An instance capability never grants schedule access, even for in-process callers.
	if _, shared := auth.ShareContextFrom(ctx); shared {
		return "", serviceerrors.NewPermissionDenied("Instance shares do not grant schedule access", nil)
	}
	principal := session.Principal()
	if err := s.authorizer.Check(ctx, principal, verb, auth.Resource{Type: "ScheduledRun", Name: name}); err != nil {
		return "", serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return principal.User.ID, nil
}

func (s *Service) authorizeTarget(ctx context.Context, schedule *apiv1alpha1.ScheduledRun) error {
	session, _ := auth.AuthSessionFrom(ctx) // Each public operation authorizes before target access.
	return AuthorizeTarget(ctx, s.authorizer, session.Principal(), schedule)
}

// AuthorizeTarget checks the target permissions shared by schedule acceptance and execution.
func AuthorizeTarget(ctx context.Context, authorizer auth.Authorizer, principal auth.Principal, schedule *apiv1alpha1.ScheduledRun) error {
	for _, resource := range []struct {
		kind, name string
		verb       auth.Verb
	}{
		{"Harness", schedule.Harness.Namespace + "/" + schedule.Harness.Name, auth.VerbGet},
		{"AgentTemplate", schedule.AgentTemplate.Namespace + "/" + schedule.AgentTemplate.Name, auth.VerbGet},
		{"AgentInstance", "", auth.VerbCreate},
	} {
		if err := authorizer.Check(ctx, principal, resource.verb, auth.Resource{Type: resource.kind, Name: resource.name}); err != nil {
			return serviceerrors.NewPermissionDenied("Not authorized to run target pair", err)
		}
	}
	return nil
}

func (s *Service) validateTarget(ctx context.Context, schedule *apiv1alpha1.ScheduledRun) error {
	harness := &v1alpha3.Harness{}
	template := &v1alpha3.AgentTemplate{}
	for _, target := range []client.Object{harness, template} {
		name := schedule.Harness.Name
		if target == template {
			name = schedule.AgentTemplate.Name
		}
		if err := s.kube.Get(ctx, types.NamespacedName{Namespace: schedule.Harness.Namespace, Name: name}, target); err != nil {
			if apierrors.IsNotFound(err) {
				return serviceerrors.NewFailedPrecondition("Target pair does not exist", err)
			}
			return serviceerrors.NewInternal("Failed to resolve target pair", err)
		}
	}
	if harness.Spec.AllowedAgentTemplates == nil {
		return serviceerrors.NewFailedPrecondition("Harness does not admit AgentTemplate", nil)
	}
	selector, err := metav1.LabelSelectorAsSelector(&harness.Spec.AllowedAgentTemplates.Selector)
	if err != nil {
		return serviceerrors.NewFailedPrecondition("Invalid Harness admission selector", err)
	}
	if !selector.Matches(labels.Set(template.Labels)) {
		return serviceerrors.NewFailedPrecondition("Harness does not admit AgentTemplate", nil)
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

func listQuery(request ListRequest, creator string) (database.ScheduledRunQuery, error) {
	limit := request.PageSize
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return database.ScheduledRunQuery{}, serviceerrors.NewInvalidArgument("Invalid page size", nil)
	}
	after := ""
	if request.PageToken != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(request.PageToken)
		if err != nil {
			return database.ScheduledRunQuery{}, serviceerrors.NewInvalidArgument("Invalid page token", err)
		}
		id, err := uuid.Parse(string(decoded))
		if err != nil {
			return database.ScheduledRunQuery{}, serviceerrors.NewInvalidArgument("Invalid page token", err)
		}
		after = id.String()
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
	case errors.Is(err, database.ErrScheduledRunConflict):
		return serviceerrors.NewAborted("Scheduled run changed; reload before updating", err)
	case errors.Is(err, database.ErrScheduledRunTargetNotReady):
		return serviceerrors.NewFailedPrecondition("Target pair has no ready prepared revision", err)
	case errors.Is(err, database.ErrScheduledRunDeleted):
		return serviceerrors.NewFailedPrecondition("Scheduled run was deleted", err)
	default:
		return serviceerrors.NewInternal("Scheduled run operation failed", err)
	}
}
