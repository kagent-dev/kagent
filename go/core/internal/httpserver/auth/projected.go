package auth

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// NewSchedulerSession identifies the in-process scheduler independently of its
// outbound credential. The projected file is trusted pod configuration; only
// downstream receivers authenticate its token.
func NewSchedulerSession(ctx context.Context, namespace, credentialPath, credentialAudience string) (auth.Session, error) {
	if namespace == "" || credentialPath == "" || credentialAudience == "" || credentialAudience == serviceAccountAudience {
		return nil, errors.New("scheduler requires a namespace and a downstream credential with a separate audience")
	}
	source := &projectedTokenSource{path: credentialPath, audience: credentialAudience}
	if _, err := source.Token(ctx); err != nil {
		return nil, err
	}
	return auth.NewSchedulerSession(namespace, source), nil
}

type projectedTokenSource struct {
	path     string
	audience string
}

var _ auth.TokenSource = (*projectedTokenSource)(nil)

func (s *projectedTokenSource) Token(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(s.path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return "", errors.New("projected outbound credential unavailable")
	}
	token := strings.TrimSpace(string(b))
	// This local check catches misconfigured projections and expired credentials.
	// It does not authenticate the scheduler or verify the token's signature.
	claims, err := parseJWTPayload(token)
	if err != nil {
		return "", errors.New("invalid outbound credential")
	}
	audiences, err := jwt.MapClaims(claims).GetAudience()
	if err != nil || len(audiences) != 1 || audiences[0] != s.audience {
		return "", errors.New("outbound credential must have only the configured downstream audience")
	}
	if err := jwt.NewValidator(jwt.WithExpirationRequired()).Validate(jwt.MapClaims(claims)); err != nil {
		return "", ErrUnauthenticated
	}
	return token, nil
}
