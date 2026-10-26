package auth

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// ProjectedSession authenticates the scheduler with its inbound token, then binds
// a separately audience-scoped outbound credential to that verified principal.
// Neither the runtime nor downstream services receive the inbound token.
func (p *ServiceAccountProvider) ProjectedSession(ctx context.Context, identityPath, credentialPath, credentialAudience string) (auth.Session, error) {
	if identityPath == "" || credentialPath == "" || credentialAudience == "" || credentialAudience == serviceAccountAudience {
		return nil, errors.New("scheduler requires separate inbound and outbound credentials and audiences")
	}
	token, err := readProjectedToken(identityPath)
	if err != nil {
		return nil, err
	}
	principal, err := p.verify(ctx, token, serviceAccountAudience)
	if err != nil {
		return nil, err
	}
	source := &projectedTokenSource{provider: p, path: credentialPath, principalID: principal.ID, audience: credentialAudience}
	if _, err := source.Token(ctx); err != nil {
		return nil, err
	}
	return auth.NewCredentialSession(principal, source), nil
}

type projectedTokenSource struct {
	provider    *ServiceAccountProvider
	path        string
	principalID string
	audience    string
}

var _ auth.TokenSource = (*projectedTokenSource)(nil)

func readProjectedToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return "", errors.New("projected invocation credential unavailable")
	}
	return strings.TrimSpace(string(b)), nil
}

func (s *projectedTokenSource) Token(ctx context.Context) (string, error) {
	token, err := readProjectedToken(s.path)
	if err != nil {
		return "", err
	}
	principal, err := s.provider.verify(ctx, token, s.audience)
	if err != nil {
		return "", err
	}
	if principal.ID != s.principalID {
		return "", errors.New("projected invocation credential identity changed")
	}
	// TokenReview verified this exact token. Reject multi-audience credentials
	// that would also be usable for inbound Kagent access.
	claims, err := parseJWTPayload(token)
	if err != nil {
		return "", errors.New("invalid outbound credential")
	}
	audiences, err := jwt.MapClaims(claims).GetAudience()
	if err != nil || slices.Contains(audiences, serviceAccountAudience) {
		return "", errors.New("outbound credential grants inbound Kagent access")
	}
	return token, nil
}
