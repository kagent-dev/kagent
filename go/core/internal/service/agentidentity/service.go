// Package agentidentity resolves metadata using narrowly scoped Actor credentials.
package agentidentity

import (
	"context"
	"errors"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

const Audience = "kagent-agent-info"

type store interface {
	GetSessionForRuntime(context.Context, string, string) (*apiv1.Session, error)
	GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
}

var _ store = (*database.Client)(nil)

type Service struct {
	verifier *substrate.ActorJWTVerifier
	store    store
	kube     client.Reader
}

func NewService(verifier *substrate.ActorJWTVerifier, store store, kube client.Reader) *Service {
	return &Service{verifier: verifier.ForAudience(Audience), store: store, kube: kube}
}

type authenticatedActorKey struct{}

// Authenticate is used only for the metadata lookup RPC. The verified identity
// carries neither a caller principal nor a runtime binding into other services.
func (s *Service) Authenticate(ctx context.Context, headers http.Header) (context.Context, error) {
	values := headers.Values("Authorization")
	if len(values) != 1 {
		return ctx, serviceerrors.NewUnauthenticated("One Agent identity Bearer credential is required", nil)
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 131072 {
		return ctx, serviceerrors.NewUnauthenticated("Invalid Agent identity credential", nil)
	}
	identity, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return ctx, serviceerrors.NewUnauthenticated("Invalid Agent identity credential", err)
	}
	return context.WithValue(ctx, authenticatedActorKey{}, identity), nil
}

// Resolve requires an authenticated metadata credential. Both Actor and Agent
// UIDs must still match the persisted binding; replacing an Agent with the same
// name does not transfer metadata authority.
func (s *Service) Resolve(ctx context.Context) (*apiv1.ResolveAgentIdentityResponse, error) {
	identity, ok := ctx.Value(authenticatedActorKey{}).(substrate.ActorIdentity)
	if !ok {
		return nil, serviceerrors.NewUnauthenticated("Agent identity credential is required", nil)
	}
	sessionID, err := identity.SessionID()
	if err != nil {
		return nil, serviceerrors.NewPermissionDenied("Actor does not belong to an Agent Session", err)
	}
	session, err := s.store.GetSessionForRuntime(ctx, sessionID, identity.ActorUID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, serviceerrors.NewPermissionDenied("Actor does not belong to an active Agent Session", err)
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to resolve Actor Session", err)
	}
	if session.GetA2AAuthority() != substrate.ActorHost(identity.Atespace, identity.ActorName, "") {
		return nil, serviceerrors.NewPermissionDenied("Actor does not belong to this Session", nil)
	}
	revision, err := s.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to resolve Agent identity", err)
	}
	if revision.AgentUID == "" || revision.Namespace == "" || revision.AgentName == "" ||
		revision.Namespace != session.GetAgent().GetNamespace() || revision.AgentName != session.GetAgent().GetName() {
		return nil, serviceerrors.NewInternal("Session has an invalid Agent identity", nil)
	}
	metadata := &metav1.PartialObjectMetadata{}
	metadata.SetGroupVersionKind(v1alpha3.GroupVersion.WithKind("Agent"))
	if err := s.kube.Get(ctx, types.NamespacedName{Namespace: revision.Namespace, Name: revision.AgentName}, metadata); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, serviceerrors.NewNotFound("Agent identity is no longer available", nil)
		}
		return nil, serviceerrors.NewInternal("Failed to read Agent metadata", err)
	}
	if string(metadata.UID) != revision.AgentUID || !metadata.DeletionTimestamp.IsZero() {
		return nil, serviceerrors.NewNotFound("Agent identity is no longer available", nil)
	}
	return &apiv1.ResolveAgentIdentityResponse{
		Agent: &apiv1.AgentIdentity{
			Namespace:   metadata.Namespace,
			Name:        metadata.Name,
			Uid:         string(metadata.UID),
			Labels:      metadata.Labels,
			Annotations: metadata.Annotations,
		},
		SessionId:       session.GetId(),
		RuntimeRevision: session.GetPreparedRevision(),
	}, nil
}
