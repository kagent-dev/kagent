package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/protobuf/proto"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Config struct {
	Store      *database.Client
	Kube       client.Client
	Authorizer auth.Authorizer
	Actors     substrate.LifecycleClient
	Guests     *GuestDialer
	DefaultTTL time.Duration
	MaxTTL     time.Duration
}

type Service struct {
	config Config
}

func NewService(config Config) (*Service, error) {
	if config.DefaultTTL <= 0 || config.DefaultTTL > config.MaxTTL || config.MaxTTL > 24*time.Hour {
		return nil, fmt.Errorf("invalid sandbox lifetime policy")
	}
	return &Service{config: config}, nil
}

func (s *Service) principal(ctx context.Context, verb auth.Verb, resource auth.Resource) (auth.Principal, error) {
	if _, shared := auth.ShareContextFrom(ctx); shared {
		return auth.Principal{}, serviceerrors.NewPermissionDenied("Agent shares do not grant sandbox access", nil)
	}
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session.Principal().User.ID == "" {
		return auth.Principal{}, serviceerrors.NewUnauthenticated("Sandbox access requires an authenticated owner", nil)
	}
	principal := session.Principal()
	if err := s.config.Authorizer.Check(ctx, principal, verb, resource); err != nil {
		return auth.Principal{}, serviceerrors.NewPermissionDenied("Sandbox access denied", err)
	}
	return principal, nil
}

func (s *Service) Create(ctx context.Context, request *apiv1alpha1.CreateSandboxRequest) (*apiv1alpha1.Sandbox, error) {
	principal, err := s.principal(ctx, auth.VerbCreate, auth.Resource{Type: "Sandbox"})
	if err != nil {
		return nil, err
	}
	ref := request.GetSandboxTemplate()
	template := &v1alpha3.SandboxTemplate{}
	err = s.config.Kube.Get(ctx, types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, template)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, serviceerrors.NewUnavailable("Cannot resolve SandboxTemplate", err)
	}
	// Existing request identity may outlive the Kubernetes template. Authorize
	// the named configuration on every retry; the store still decides its pin.
	resource := auth.Resource{Type: v1alpha3.SandboxTemplateKind, Namespace: ref.GetNamespace(), Name: ref.GetName()}
	if err == nil {
		resource.Namespace, resource.Name = template.Namespace, template.Name
	}
	if err := s.config.Authorizer.Check(ctx, principal, auth.VerbGet, resource); err != nil {
		return nil, serviceerrors.NewPermissionDenied("SandboxTemplate access denied", err)
	}
	ttl := s.config.DefaultTTL
	if request.Ttl != nil {
		ttl = request.Ttl.AsDuration()
	}
	if ttl <= 0 || ttl > s.config.MaxTTL {
		return nil, serviceerrors.NewInvalidArgument("Requested sandbox lifetime exceeds operator policy", nil)
	}
	hash, err := requestHash(request)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	instance, _, err := s.config.Store.CreateSandbox(ctx, &apiv1alpha1.Sandbox{
		Id: id.String(), Creator: principal.User.ID, Name: request.Name, SandboxTemplate: ref,
	}, request.RequestId, database.SandboxCreateOptions{RequestHash: hash, TemplateUID: string(template.UID), TTL: ttl})
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewFailedPrecondition("SandboxTemplate has no prepared revision", err)
	}
	if err != nil {
		return nil, sandboxError(err)
	}
	return s.run(ctx, instance.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
}

func (s *Service) authorized(ctx context.Context, id string, verb auth.Verb) (*apiv1alpha1.Sandbox, error) {
	principal, err := s.principal(ctx, verb, auth.Resource{Type: "Sandbox", Name: id})
	if err != nil {
		return nil, err
	}
	instance, err := s.config.Store.GetSandbox(ctx, id, principal.User.ID)
	return instance, sandboxError(err)
}

func (s *Service) Get(ctx context.Context, id string) (*apiv1alpha1.Sandbox, error) {
	return s.authorized(ctx, id, auth.VerbGet)
}

func (s *Service) List(ctx context.Context, request *apiv1alpha1.ListSandboxesRequest) (*apiv1alpha1.ListSandboxesResponse, error) {
	principal, err := s.principal(ctx, auth.VerbList, auth.Resource{Type: "Sandbox"})
	if err != nil {
		return nil, err
	}
	limit := int(request.GetPage().GetLimit())
	if limit == 0 {
		limit = 50
	}
	if limit < 0 || limit > 200 {
		return nil, serviceerrors.NewInvalidArgument("Sandbox page size must be between 1 and 200", nil)
	}
	after := ""
	if token := request.GetPage().GetPageToken(); token != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			return nil, serviceerrors.NewInvalidArgument("Invalid sandbox page token", err)
		}
		if _, err := uuid.Parse(string(decoded)); err != nil {
			return nil, serviceerrors.NewInvalidArgument("Invalid sandbox page token", err)
		}
		after = string(decoded)
	}
	instances, err := s.config.Store.ListSandboxes(ctx, principal.User.ID, after, limit+1, request.SandboxTemplate)
	if err != nil {
		return nil, sandboxError(err)
	}
	response := &apiv1alpha1.ListSandboxesResponse{Sandboxes: instances, Page: &apiv1alpha1.PageResponse{}}
	if len(instances) > limit {
		response.Sandboxes = instances[:limit]
		response.Page.NextPageToken = base64.RawURLEncoding.EncodeToString([]byte(instances[limit-1].Id))
	}
	return response, nil
}

func (s *Service) Suspend(ctx context.Context, id string) (*apiv1alpha1.Sandbox, error) {
	if _, err := s.authorized(ctx, id, auth.VerbUpdate); err != nil {
		return nil, err
	}
	return s.run(ctx, id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
}

func (s *Service) Resume(ctx context.Context, id string) (*apiv1alpha1.Sandbox, error) {
	if _, err := s.authorized(ctx, id, auth.VerbUpdate); err != nil {
		return nil, err
	}
	return s.run(ctx, id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
}

func (s *Service) Delete(ctx context.Context, id string) (*apiv1alpha1.Sandbox, error) {
	if _, err := s.authorized(ctx, id, auth.VerbDelete); err != nil {
		return nil, err
	}
	return s.run(ctx, id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
}

func requestHash(request proto.Message) ([]byte, error) {
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode sandbox request: %w", err)
	}
	hash := sha256.Sum256(data)
	return hash[:], nil
}

func sandboxError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, database.ErrNotFound):
		return serviceerrors.NewNotFound("Sandbox or process not found", err)
	case errors.Is(err, database.ErrConflict):
		return serviceerrors.NewAborted("Sandbox lifecycle attempt is in progress or was superseded; retry the intended operation", err)
	case errors.Is(err, database.ErrFailedPrecondition):
		return serviceerrors.NewFailedPrecondition("Sandbox is expired, deleted, suspended, or unavailable for this operation", err)
	case errors.Is(err, database.ErrIdempotencyConflict):
		return serviceerrors.NewAlreadyExists("request_id was used for different input", err)
	default:
		return serviceerrors.NewUnavailable("Sandbox operation failed", err)
	}
}
