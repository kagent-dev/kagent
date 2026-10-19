package taskstore

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
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
	values := headers.Values(apia2a.RuntimeAuthorizationHeader)
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
