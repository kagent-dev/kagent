package auth

import (
	"context"
	"net/http"
	"net/url"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// DenyAuthenticator rejects every user credential. It is the user provider for
// deployments where only verified service-account tokens and Substrate Actor
// credentials may call the controller; those providers wrap it and handle
// their credentials before it is consulted.
type DenyAuthenticator struct{}

func (DenyAuthenticator) Authenticate(context.Context, http.Header, url.Values) (auth.Session, error) {
	return nil, ErrUnauthenticated
}

// UpstreamAuth forwards sessions that a wrapping provider verified, such as
// service-account sessions.
func (DenyAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, destinationSessionID string) error {
	return auth.ForwardCredential(r, session)
}
