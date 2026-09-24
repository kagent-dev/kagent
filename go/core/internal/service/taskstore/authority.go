package taskstore

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Authenticator normally verifies actor JWTs injected by Substrate's egress gateway. It
// never issues credentials. The trusted issuer and verification keys come from
// the operator's Substrate configuration, never from an incoming token.
// Wiring discovery/JWKS and egress injection requires Substrate issue #1660.
type Authenticator struct {
	issuer   string
	keys     jwt.Keyfunc
	insecure bool
}

var _ auth.AuthProvider = (*Authenticator)(nil)

func NewAuthenticator(issuer string, keys jwt.Keyfunc) (*Authenticator, error) {
	if issuer == "" || keys == nil {
		return nil, fmt.Errorf("substrate JWT issuer and verification keys are required")
	}
	return &Authenticator{issuer: issuer, keys: keys}, nil
}

// NewInsecureAuthenticator trusts a caller-supplied actor identity. It exists
// only for isolated E2E tests until Substrate injects actor credentials (#1660).
// Instance, atespace and actor UID checks still run in the TaskStore service.
func NewInsecureAuthenticator() *Authenticator {
	return &Authenticator{insecure: true}
}

// actorClaims follows Substrate's current actoridjwt wire format. The actor UID
// distinguishes replacement actors with the same name. The service binds it to
// the UID saved when the API created this instance's actor.
type actorClaims struct {
	jwt.RegisteredClaims
	Actor struct {
		Atespace string `json:"atespace"`
		Name     string `json:"actorName"`
		UID      string `json:"actorUID"`
	} `json:"ate.dev"`
}

func (a *Authenticator) Authenticate(_ context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	claims := &actorClaims{}
	if a.insecure {
		values := headers.Values(apia2a.InsecureRuntimeIdentityHeader)
		if len(values) != 1 {
			return nil, fmt.Errorf("one insecure runtime identity header is required")
		}
		parts := strings.Split(values[0], "/")
		if len(parts) != 3 {
			return nil, fmt.Errorf("insecure runtime identity must be atespace/actor-name/actor-UID")
		}
		claims.Actor.Atespace, claims.Actor.Name, claims.Actor.UID = parts[0], parts[1], parts[2]
	} else {
		value := headers.Get("Authorization")
		if !strings.HasPrefix(value, "Bearer ") {
			return nil, fmt.Errorf("substrate actor credential is required")
		}
		_, err := jwt.ParseWithClaims(strings.TrimPrefix(value, "Bearer "), claims, a.keys,
			jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg(), jwt.SigningMethodRS256.Alg()}),
			jwt.WithIssuer(a.issuer), jwt.WithAudience("kagent-task-store"), jwt.WithExpirationRequired(),
		)
		if err != nil {
			return nil, fmt.Errorf("invalid Substrate actor credential: %w", err)
		}
	}
	id, ok := strings.CutPrefix(claims.Actor.Name, "ai-")
	if !ok || claims.Actor.Atespace == "" || claims.Actor.UID == "" {
		return nil, fmt.Errorf("incomplete Substrate actor identity")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("invalid runtime instance identity: %w", err)
	}
	return runtimeSession{instanceID: id, atespace: claims.Actor.Atespace, actorUID: claims.Actor.UID}, nil
}

func (*Authenticator) UpstreamAuth(*http.Request, auth.Session, auth.Principal) error {
	return fmt.Errorf("runtime authentication cannot forward public credentials")
}

type runtimeSession struct{ instanceID, atespace, actorUID string }

func (s runtimeSession) Principal() auth.Principal {
	return auth.Principal{Agent: auth.Agent{ID: s.instanceID}}
}
