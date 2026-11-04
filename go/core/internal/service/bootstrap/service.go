// Package bootstrap authorizes configuration reads before a Session exists.
package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"strings"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	contract "github.com/kagent-dev/kagent/go/api/bootstrap"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

type store interface {
	GetBootstrapBinding(context.Context, string, string) (*database.BootstrapBinding, error)
	GetBootstrapBundle(context.Context, database.BootstrapBinding) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error)
}
type backend interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error)
}

var (
	_ store   = (*database.Client)(nil)
	_ backend = (*substrate.Client)(nil)
)

type Service struct {
	verifier *substrate.ActorJWTVerifier
	store    store
	backend  backend
}

func NewService(verifier *substrate.ActorJWTVerifier, store store, backend backend) *Service {
	return &Service{verifier: verifier.ForAudience(contract.Audience), store: store, backend: backend}
}

type actorKey struct{}

// Authenticate grants only bootstrap identity; it never creates user or Session authority.
func (s *Service) Authenticate(ctx context.Context, headers http.Header) (context.Context, error) {
	values := headers.Values("Authorization")
	if len(values) != 1 {
		return ctx, serviceerrors.NewUnauthenticated("One bootstrap credential is required", nil)
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 131072 {
		return ctx, serviceerrors.NewUnauthenticated("Invalid bootstrap credential", nil)
	}
	actor, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return ctx, serviceerrors.NewUnauthenticated("Invalid bootstrap credential", err)
	}
	return context.WithValue(ctx, actorKey{}, actor), nil
}

// Get returns only the authenticated actor's associated template bundle.
func (s *Service) Get(ctx context.Context) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error) {
	identity, ok := ctx.Value(actorKey{}).(substrate.ActorIdentity)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("Bootstrap actor identity is required", nil)
	}
	actor, err := s.backend.GetActor(ctx, identity.Atespace, identity.ActorName)
	if status.Code(err) == codes.NotFound {
		return nil, serviceerrors.NewPermissionDenied("Bootstrap actor no longer exists", nil)
	}
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to resolve bootstrap actor", err)
	}
	if actor.GetMetadata().GetUid() != identity.ActorUID || actor.GetMetadata().GetAtespace() != identity.Atespace || actor.GetMetadata().GetName() != identity.ActorName {
		return nil, serviceerrors.NewPermissionDenied("Bootstrap actor identity changed", nil)
	}
	ref := actor.GetActorTemplate()
	if ref.GetAtespace() == "" || ref.GetName() == "" {
		return nil, serviceerrors.NewPermissionDenied("Actor has no associated template", nil)
	}
	binding, err := s.store.GetBootstrapBinding(ctx, ref.GetAtespace(), ref.GetName())
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewPermissionDenied("Actor template has no bootstrap configuration", nil)
	}
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to resolve bootstrap binding", err)
	}
	template, err := s.backend.GetActorTemplate(ctx, ref.GetAtespace(), ref.GetName())
	if err != nil {
		return nil, serviceerrors.NewUnavailable("Failed to resolve bootstrap template", err)
	}
	digest, err := substrate.ActorTemplateSpecDigest(template)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to verify bootstrap template", err)
	}
	if digest != binding.ActorTemplateSpecDigest {
		return nil, serviceerrors.NewPermissionDenied("Bootstrap template does not match its revision", nil)
	}
	if binding.ActorTemplateUID == "" || template.GetMetadata().GetUid() != binding.ActorTemplateUID {
		// Initial creation and golden rebuilds can run before their UID is recorded.
		return nil, serviceerrors.NewUnavailable("Bootstrap template binding is pending", nil)
	}
	bundle, err := s.store.GetBootstrapBundle(ctx, *binding)
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewUnavailable("Bootstrap binding changed; retry initialization", nil)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to read bootstrap configuration", err)
	}
	return bundle, nil
}
