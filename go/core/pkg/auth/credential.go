package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
)

// PrincipalID namespaces a provider's subject without exposing its syntax to
// consumers. Authorities identify authentication sources, not rotating keys.
func PrincipalID(authority, subject string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(authority)) + "." + base64.RawURLEncoding.EncodeToString([]byte(subject))
}

// TokenSource supplies the current bearer credential. An empty token means the
// authentication mechanism has no bearer credential; failures never select a
// different source. Implementations must not expose credentials through errors.
type TokenSource interface {
	Token(context.Context) (string, error)
}

type credentialSession struct {
	principal Principal
	source    TokenSource
}

var _ Session = (*credentialSession)(nil)
var _ TokenSource = (*credentialSession)(nil)

// NewCredentialSession binds a verified principal to its renewable credential.
// The authenticator is responsible for ensuring the source retains this identity.
func NewCredentialSession(principal Principal, source TokenSource) Session {
	return &credentialSession{principal: principal, source: source}
}

func (s *credentialSession) Principal() Principal { return s.principal }

func (s *credentialSession) Token(ctx context.Context) (string, error) {
	if s.source == nil {
		return "", nil
	}
	return s.source.Token(ctx)
}

// ForwardCredential applies a caller's current credential to an authorized
// upstream request. Source-less internal calls carry no bearer token.
func ForwardCredential(request *http.Request, session Session) error {
	request.Header.Del("Authorization")
	request.Header.Del("X-User-Id")
	if session == nil {
		return nil
	}
	request.Header.Set("X-User-Id", session.Principal().ID)
	if source, ok := session.(TokenSource); ok {
		token, err := source.Token(request.Context())
		if err != nil {
			return fmt.Errorf("resolve invocation credential: %w", err)
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return nil
}
