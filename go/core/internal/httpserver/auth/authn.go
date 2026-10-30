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
	P auth.Principal
	bearerCredential
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
		bearerCredential: bearerCredential{authHeader: authHeader},
	}, nil
}

func (a *InsecureAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, destinationSessionID string) error {
	return auth.ForwardCredential(r, session)
}

// bearerCredential is the immutable credential captured for one request.
type bearerCredential struct {
	authHeader string
	expiresAt  time.Time
}

var _ auth.TokenSource = bearerCredential{}

func (s bearerCredential) Token(ctx context.Context) (string, error) {
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

func jwtExpiration(claims map[string]any) (time.Time, error) {
	var expiresAt time.Time
	if raw, exists := claims["exp"]; exists {
		seconds, ok := raw.(float64)
		if !ok || seconds <= float64(time.Now().Unix()) {
			return time.Time{}, ErrUnauthenticated
		}
		expiresAt = time.Unix(int64(seconds), 0)
	}
	return expiresAt, nil
}
