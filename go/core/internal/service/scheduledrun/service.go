package scheduledrun

import (
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"slices"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/scheduling"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type serviceStore interface {
	GetScheduledRun(context.Context, uuid.UUID, string) (*database.ScheduledRun, error)
	TriggerScheduledRun(context.Context, uuid.UUID, string, string, int64) (*apiv1.ScheduledRunExecution, error)
	GetScheduledRunExecution(context.Context, uuid.UUID, string) (*apiv1.ScheduledRunExecution, error)
	ListScheduledRunExecutions(context.Context, database.ScheduledRunExecutionQuery) ([]*apiv1.ScheduledRunExecution, error)
}

type Service struct {
	namespaces []string
	store      serviceStore
	kube       client.Client
	resources  *kubecrud.Service[*v1alpha3.ScheduledRun, *v1alpha3.ScheduledRunList]
	authorizer auth.CollectionAuthorizer
}

func NewService(store serviceStore, kube client.Client, authorizer auth.CollectionAuthorizer, namespaces []string) *Service {
	return &Service{namespaces: slices.Clone(namespaces), store: store, kube: kube, authorizer: authorizer, resources: kubecrud.NewService(kube, authorizer, &v1alpha3.ScheduledRun{}, &v1alpha3.ScheduledRunList{}, v1alpha3.ScheduledRunKind)}
}

func (s *Service) Create(ctx context.Context, resource *v1alpha3.ScheduledRun) (*v1alpha3.ScheduledRun, error) {
	if _, err := principal(ctx); err != nil {
		return nil, err
	}
	if resource == nil {
		return nil, serviceerrors.NewInvalidArgument("ScheduledRun is required", nil)
	}
	if err := scheduling.Validate(resource.Spec); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid schedule", err)
	}
	if err := s.resources.Authorize(ctx, auth.VerbCreate, client.ObjectKeyFromObject(resource)); err != nil {
		return nil, err
	}
	resource = resource.DeepCopy()
	resource.Status = v1alpha3.ScheduledRunStatus{}
	resource.Finalizers = nil
	return s.resources.Create(ctx, resource)
}

func (s *Service) Get(ctx context.Context, ref types.NamespacedName) (*v1alpha3.ScheduledRun, error) {
	if _, err := principal(ctx); err != nil {
		return nil, err
	}
	return s.resources.Get(ctx, ref)
}

func (s *Service) List(ctx context.Context, namespace string, page *apiv1.PageRequest) ([]*v1alpha3.ScheduledRun, string, error) {
	if _, err := principal(ctx); err != nil {
		return nil, "", err
	}
	resources, err := s.resources.List(ctx, namespace)
	if err != nil {
		return nil, "", err
	}
	return kubecrud.Page(resources, int(page.GetLimit()), page.GetPageToken())
}

func (s *Service) Update(ctx context.Context, incoming *v1alpha3.ScheduledRun) (*v1alpha3.ScheduledRun, error) {
	if _, err := principal(ctx); err != nil {
		return nil, err
	}
	if incoming == nil {
		return nil, serviceerrors.NewInvalidArgument("ScheduledRun is required", nil)
	}
	if err := scheduling.Validate(incoming.Spec); err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid schedule", err)
	}
	if incoming.ResourceVersion == "" {
		return nil, serviceerrors.NewInvalidArgument("metadata.resourceVersion is required", nil)
	}
	ref := client.ObjectKeyFromObject(incoming)
	if err := s.resources.Authorize(ctx, auth.VerbUpdate, ref); err != nil {
		return nil, err
	}
	existing := &v1alpha3.ScheduledRun{}
	if err := s.kube.Get(ctx, ref, existing); err != nil {
		return nil, kubeError(err)
	}
	if incoming.UID != "" && incoming.UID != existing.UID {
		return nil, serviceerrors.NewAborted("ScheduledRun was recreated", nil)
	}
	existing.Spec = *incoming.Spec.DeepCopy()
	existing.Labels = maps.Clone(incoming.Labels)
	existing.Annotations = maps.Clone(incoming.Annotations)
	existing.ResourceVersion = incoming.ResourceVersion
	if err := s.kube.Update(ctx, existing); err != nil {
		return nil, kubeError(err)
	}
	return existing, nil
}

func (s *Service) Delete(ctx context.Context, ref types.NamespacedName, uid types.UID, version string) error {
	if _, err := principal(ctx); err != nil {
		return err
	}
	if uid == "" || version == "" {
		return serviceerrors.NewInvalidArgument("UID and resourceVersion are required", nil)
	}
	if err := s.resources.Authorize(ctx, auth.VerbDelete, ref); err != nil {
		return err
	}
	resource := &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name}}
	return kubeError(s.kube.Delete(ctx, resource, client.Preconditions{UID: &uid, ResourceVersion: &version}))
}

func (s *Service) Trigger(ctx context.Context, id uuid.UUID, requestID string) (*apiv1.ScheduledRunExecution, error) {
	caller, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	schedule, err := s.store.GetScheduledRun(ctx, id, "")
	if err != nil {
		return nil, storeError(err)
	}
	if err := s.authorize(ctx, auth.VerbCreate, "ScheduledRunInvocation", schedule.Ref); err != nil {
		return nil, err
	}
	if len(s.namespaces) != 0 && !slices.Contains(s.namespaces, schedule.Ref.Namespace) {
		return nil, serviceerrors.NewFailedPrecondition("ScheduledRun namespace is not watched by this controller", nil)
	}
	resource := &v1alpha3.ScheduledRun{}
	err = s.kube.Get(ctx, schedule.Ref, resource)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, kubeError(err)
	}
	// Only an exact, live resource can accept a new manual occurrence. An old
	// request may still replay after deletion or edits. Fence new reservations to
	// the valid snapshot whose target we authorized, even if reconciliation races
	// this read or repairs a disabled projection at the same observed generation.
	var generation int64
	if err == nil && !schedule.Disabled && string(resource.UID) == id.String() && resource.DeletionTimestamp.IsZero() && resource.Generation == schedule.Generation {
		generation = schedule.Generation
	}
	result, err := s.store.TriggerScheduledRun(ctx, id, caller.ID, requestID, generation)
	return result, storeError(err)
}

func (s *Service) GetExecution(ctx context.Context, id uuid.UUID) (*apiv1.ScheduledRunExecution, error) {
	if _, err := principal(ctx); err != nil {
		return nil, err
	}
	result, err := s.store.GetScheduledRunExecution(ctx, id, "")
	if err != nil {
		return nil, storeError(err)
	}
	if err := s.authorize(ctx, auth.VerbGet, "ScheduledRunHistory", types.NamespacedName{Namespace: result.GetScheduledRun().GetNamespace(), Name: result.GetScheduledRun().GetName()}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListExecutions(ctx context.Context, id uuid.UUID, page *apiv1.PageRequest) ([]*apiv1.ScheduledRunExecution, string, error) {
	if _, err := principal(ctx); err != nil {
		return nil, "", err
	}
	query, err := listQuery(page, "")
	if err != nil {
		return nil, "", err
	}
	matcher, err := kubeauth.CollectionMatcher(ctx, s.authorizer, "ScheduledRunHistory")
	if err != nil {
		return nil, "", err
	}
	if matcher.Empty() {
		return []*apiv1.ScheduledRunExecution{}, "", nil
	}
	schedule, err := s.store.GetScheduledRun(ctx, id, "")
	if err != nil {
		return nil, "", storeError(err)
	}
	if !matcher.Matches(&metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: schedule.Ref.Namespace, Name: schedule.Ref.Name}}) {
		return []*apiv1.ScheduledRunExecution{}, "", nil
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

func principal(ctx context.Context) (auth.Principal, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session.Principal().ID == "" {
		return auth.Principal{}, serviceerrors.NewUnauthenticated("Authentication is required", nil)
	}
	if _, shared := auth.ShareContextFrom(ctx); shared || session.Principal().RuntimeBinding != nil {
		return auth.Principal{}, serviceerrors.NewPermissionDenied("Session capabilities do not grant schedule access", nil)
	}
	return session.Principal(), nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, kind string, ref types.NamespacedName) error {
	caller, err := principal(ctx)
	if err != nil {
		return err
	}
	if err := s.authorizer.Check(ctx, caller, verb, auth.Resource{Type: kind, Namespace: ref.Namespace, Name: ref.Name}); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return nil
}

func kubeError(err error) error {
	switch {
	case err == nil:
		return nil
	case apierrors.IsNotFound(err):
		return serviceerrors.NewNotFound("ScheduledRun not found", err)
	case apierrors.IsConflict(err):
		return serviceerrors.NewAborted("ScheduledRun changed; reload before retrying", err)
	case apierrors.IsAlreadyExists(err):
		return serviceerrors.NewAlreadyExists("ScheduledRun already exists", err)
	case apierrors.IsInvalid(err):
		return serviceerrors.NewInvalidArgument("Invalid ScheduledRun", err)
	default:
		return serviceerrors.NewInternal("ScheduledRun operation failed", err)
	}
}

func listQuery(page *apiv1.PageRequest, creator string) (database.ScheduledRunQuery, error) {
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
