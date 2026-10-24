package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type SimpleSession struct {
	P          auth.Principal
	authHeader string
	expiresAt  time.Time
}

func (s *SimpleSession) Principal() auth.Principal {
	return s.P
}

type InsecureAuthenticator struct{}

func (a *InsecureAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	userID := query.Get("user_id")
	if userID == "" {
		userID = reqHeaders.Get("X-User-Id")
	}
	if userID == "" {
		userID = "admin@kagent.dev"
	}
	authHeader := reqHeaders.Get("Authorization")

	return &SimpleSession{
		P: auth.Principal{
			ID: userID,
		},
		authHeader: authHeader,
	}, nil
}

func (a *InsecureAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, destinationSessionID string) error {
	return auth.ForwardCredential(r, session)
}

func (s *SimpleSession) Token(ctx context.Context) (string, error) {
	if !s.expiresAt.IsZero() && !time.Now().Before(s.expiresAt) {
		return "", ErrUnauthenticated
	}
	if s.authHeader == "" {
		return "", nil
	}
	scheme, token, ok := strings.Cut(s.authHeader, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", ErrUnauthenticated
	}
	return token, nil
}
