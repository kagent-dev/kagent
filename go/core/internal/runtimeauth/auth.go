// Package runtimeauth authenticates Substrate actors without granting user authority.
package runtimeauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type actorJWTVerifier interface {
	Verify(context.Context, string) (substrate.ActorIdentity, error)
}

// Authenticator verifies runtime credentials. Session, atespace, and recorded
// Actor UID authorization remain in the service.
type Authenticator struct{ verifier actorJWTVerifier }

func NewAuthenticator(verifier actorJWTVerifier) *Authenticator {
	return &Authenticator{verifier: verifier}
}

var _ auth.AuthProvider = (*Authenticator)(nil)
var _ actorJWTVerifier = (*substrate.ActorJWTVerifier)(nil)

func (a *Authenticator) Authenticate(ctx context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	values := headers.Values("authorization")
	if len(values) != 1 {
		return nil, fmt.Errorf("one runtime authorization header is required")
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return nil, fmt.Errorf("runtime authorization requires a Bearer token")
	}
	identity, err := a.verifier.Verify(ctx, token)
	if err != nil {
		logging.FromContext(ctx).DebugContext(ctx, "runtime actor authentication failed", "error", err)
		return nil, err
	}
	id, ok := strings.CutPrefix(identity.ActorName, "session-")
	if !ok || identity.Atespace == "" || identity.ActorUID == "" {
		return nil, fmt.Errorf("incomplete Substrate actor identity")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("invalid runtime session identity: %w", err)
	}
	return runtimeSession{sessionID: id, atespace: identity.Atespace, actorUID: identity.ActorUID}, nil
}

func (*Authenticator) UpstreamAuth(*http.Request, auth.Session, auth.Principal) error {
	return fmt.Errorf("runtime authentication cannot forward public credentials")
}

type runtimeSession struct{ sessionID, atespace, actorUID string }

func (s runtimeSession) Principal() auth.Principal {
	return auth.Principal{Agent: auth.Agent{ID: s.sessionID}}
}

type SessionStore interface {
	GetSessionForRuntime(context.Context, string, string) (*apiv1.Session, error)
}

// Session resolves the current session binding, including the recorded actor UID.
// It never accepts caller-supplied user, agent, or share headers as authority.
func Session(ctx context.Context, store SessionStore) (*apiv1.Session, error) {
	authSession, _ := auth.AuthSessionFrom(ctx)
	identity, ok := authSession.(runtimeSession)
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "runtime actor identity is required")
	}
	session, err := store.GetSessionForRuntime(ctx, identity.sessionID, identity.actorUID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, status.Error(codes.PermissionDenied, "actor does not belong to an active session")
	}
	if err != nil {
		return nil, fmt.Errorf("resolve actor session: %w", err)
	}
	if session.A2AAuthority != substrate.ActorHost(identity.atespace, substrate.ActorName(identity.sessionID), "") {
		return nil, status.Error(codes.PermissionDenied, "actor does not belong to this session")
	}
	return session, nil
}
