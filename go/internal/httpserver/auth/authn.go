package auth

import (
	"context"
	"net/http"
)

var (
	sessionKey = &struct{}{}
)

// type Authorizer interface {
// 	AuthorizeRequest(req *http.Request) error
// 	AuthorizeRequest2(principal string, verb string, resource string) error
// }

// AuthN
func AuthSessionFrom(ctx context.Context) (*Session, bool) {
	v, ok := ctx.Value(sessionKey).(*Session)
	return v, ok && v != nil
}

type Principal struct {
	User  string
	Agent string
}
type Session struct {
	Principal Principal
}

func AuthnMiddleware(authn func(r *http.Request) *Session) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := authn(r)
			if session != nil {
				r = r.WithContext(context.WithValue(r.Context(), sessionKey, session))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func NoopAuthn(r *http.Request) *Session {
	return nil
}

func TestAuthn(r *http.Request) *Session {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		return nil
	}
	return &Session{
		Principal: Principal{
			User: userID,
		},
	}
}

//// AuthZ

type Authorizer interface {
	Check(principal Principal, verb string, resource string) error
}
