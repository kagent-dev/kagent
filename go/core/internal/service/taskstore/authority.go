package taskstore

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Authenticator temporarily trusts an unsigned actor identity header. Replace
// this with Substrate-issued actor JWT verification when #1660 is available.
// Session, atespace, and recorded actor UID checks remain in the service.
type Authenticator struct{}

var _ auth.AuthProvider = (*Authenticator)(nil)

func (*Authenticator) Authenticate(_ context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	values := headers.Values(apia2a.InsecureRuntimeIdentityHeader)
	if len(values) != 1 {
		return nil, fmt.Errorf("one runtime identity header is required")
	}
	parts := strings.Split(values[0], "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("runtime identity must be atespace/actor-name/actor-UID")
	}
	id, ok := strings.CutPrefix(parts[1], "session-")
	if !ok || parts[0] == "" || parts[2] == "" {
		return nil, fmt.Errorf("incomplete Substrate actor identity")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("invalid runtime session identity: %w", err)
	}
	return runtimeSession{sessionID: id, atespace: parts[0], actorUID: parts[2]}, nil
}

func (*Authenticator) UpstreamAuth(*http.Request, auth.Session, auth.Principal) error {
	return fmt.Errorf("runtime authentication cannot forward public credentials")
}

type runtimeSession struct{ sessionID, atespace, actorUID string }

func (s runtimeSession) Principal() auth.Principal {
	return auth.Principal{Agent: auth.Agent{ID: s.sessionID}}
}
