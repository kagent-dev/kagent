package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrUnauthenticated = errors.New("unauthenticated: missing or invalid Authorization header")

// serviceAccountPrefix starts the user name of every Kubernetes
// ServiceAccount, and the sub claim of every ServiceAccount token.
const serviceAccountPrefix = "system:serviceaccount:"

// tokenReviewTimeout bounds one TokenReview, so that an API server that stops
// answering refuses the call within seconds.
const tokenReviewTimeout = 5 * time.Second

// TokenReviewer creates TokenReviews; client-go's typed client satisfies it.
type TokenReviewer interface {
	Create(context.Context, *authenticationv1.TokenReview, metav1.CreateOptions) (*authenticationv1.TokenReview, error)
}

type ProxyAuthenticator struct {
	userIDClaim string
	groupsClaim string
	// claimRequired refuses a token without userIDClaim instead of falling
	// back to sub.
	claimRequired bool
	// tokenReviews, when set, authenticates a ServiceAccount token with the
	// API server instead of trusting its claims; tokenAudience is the
	// audience the token must carry.
	tokenReviews  TokenReviewer
	tokenAudience string
	// sessionReader, when set, is the user name of the ServiceAccount
	// admitted as the session reader (WithSessionReader).
	sessionReader string
}

// ProxyAuthenticatorOption configures a ProxyAuthenticator.
type ProxyAuthenticatorOption func(*ProxyAuthenticator)

// WithGroupsClaim reads the user's groups from claim, as the API server's
// --oidc-groups-claim. Without it a user carries no groups.
func WithGroupsClaim(claim string) ProxyAuthenticatorOption {
	return func(a *ProxyAuthenticator) { a.groupsClaim = claim }
}

// WithUserIDClaimRequired refuses a direct call whose token lacks the user ID
// claim, as the API server refuses it, instead of falling back to sub: under
// Kubernetes authorization the user is then never reviewed under a name the
// API server would not produce for the same token.
func WithUserIDClaimRequired() ProxyAuthenticatorOption {
	return func(a *ProxyAuthenticator) { a.claimRequired = true }
}

// WithServiceAccountTokenReview authenticates a direct call whose token is a
// Kubernetes ServiceAccount token (its sub names a ServiceAccount) with a
// TokenReview for audience, instead of reading its claims: the caller is the
// user and groups the API server returns, and a token the API server does not
// authenticate as a ServiceAccount for audience is refused. A person's token
// is read as before.
func WithServiceAccountTokenReview(reviews TokenReviewer, audience string) ProxyAuthenticatorOption {
	return func(a *ProxyAuthenticator) { a.tokenReviews, a.tokenAudience = reviews, audience }
}

// WithSessionReader admits the ServiceAccount user (system:serviceaccount:
// <namespace>:<name>) as the session reader: on a call admitted for it
// (auth.AdmitSessionReader) the API server's TokenReview of its token yields
// an auth.SessionReaderSession, and every other call of it is refused with
// auth.ErrSessionReaderRefused. It takes effect only beside
// WithServiceAccountTokenReview: a token's claims never name the reader.
func WithSessionReader(user string) ProxyAuthenticatorOption {
	return func(a *ProxyAuthenticator) { a.sessionReader = user }
}

func NewProxyAuthenticator(userIDClaim string, options ...ProxyAuthenticatorOption) *ProxyAuthenticator {
	if userIDClaim == "" {
		userIDClaim = "sub"
	}
	authenticator := &ProxyAuthenticator{userIDClaim: userIDClaim}
	for _, option := range options {
		option(authenticator)
	}
	return authenticator
}

func (a *ProxyAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	authHeader := reqHeaders.Get("Authorization")
	agentID := reqHeaders.Get("X-Agent-Name")

	tokenString, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return nil, ErrUnauthenticated
	}

	// Parse JWT without validation (oauth2-proxy or k8s service account already validated)
	rawClaims, err := parseJWTPayload(tokenString)
	if err != nil {
		return nil, ErrUnauthenticated
	}

	if agentID != "" {
		// Agent call: the Bearer SA token authenticates the pod; the caller's
		// identity should be supplied explicitly via X-User-Id / user_id.
		// Fall back to the SA sub claim for direct calls to agent pods that
		// do not yet propagate the caller identity.
		userID := userIDFromRequest(reqHeaders, query)
		var groups []string
		if userID != "" {
			// The groups belong to the user the agent calls for, never to
			// the pod's own ServiceAccount below.
			if groups, err = auth.DecodeUserGroups(reqHeaders.Get(auth.UserGroupsHeader)); err != nil {
				return nil, ErrUnauthenticated
			}
		} else {
			userID, _ = rawClaims["sub"].(string)
		}
		if userID == "" {
			return nil, ErrUnauthenticated
		}
		return &SimpleSession{
			P: auth.Principal{
				User:  auth.User{ID: userID, Groups: groups},
				Agent: auth.Agent{ID: agentID},
			},
			authHeader: authHeader,
		}, nil
	}

	if subject, _ := rawClaims["sub"].(string); a.tokenReviews != nil && strings.HasPrefix(subject, serviceAccountPrefix) {
		user, err := a.reviewServiceAccountToken(ctx, tokenString)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
		}
		if a.sessionReader != "" && user.ID == a.sessionReader {
			if !auth.SessionReaderAdmitted(ctx) {
				return nil, fmt.Errorf("%w: %s", auth.ErrSessionReaderRefused, user.ID)
			}
			return auth.SessionReaderSession{P: auth.Principal{User: user, Claims: rawClaims}}, nil
		}
		return &SimpleSession{P: auth.Principal{User: user, Claims: rawClaims}, authHeader: authHeader}, nil
	}

	// Direct user call: identity comes from the OIDC JWT claims.
	userID, _ := rawClaims[a.userIDClaim].(string)
	if userID == "" && a.userIDClaim != "sub" && !a.claimRequired {
		userID, _ = rawClaims["sub"].(string)
	}
	if userID == "" {
		return nil, ErrUnauthenticated
	}
	var groups []string
	if a.groupsClaim != "" {
		// A malformed groups claim fails the token, as the API server fails it.
		if groups, err = groupsFromClaim(rawClaims[a.groupsClaim], a.groupsClaim); err != nil {
			return nil, ErrUnauthenticated
		}
	}
	return &SimpleSession{
		P: auth.Principal{
			User:   auth.User{ID: userID, Groups: groups},
			Claims: rawClaims,
		},
		authHeader: authHeader,
	}, nil
}

// reviewServiceAccountToken asks the API server who token authenticates as.
// Only a ServiceAccount is accepted: the token was sent here as one.
func (a *ProxyAuthenticator) reviewServiceAccountToken(ctx context.Context, token string) (auth.User, error) {
	review := &authenticationv1.TokenReview{Spec: authenticationv1.TokenReviewSpec{Token: token, Audiences: []string{a.tokenAudience}}}
	ctx, cancel := context.WithTimeout(ctx, tokenReviewTimeout)
	defer cancel()
	result, err := a.tokenReviews.Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return auth.User{}, fmt.Errorf("failed to review the ServiceAccount token: %w", err)
	}
	if !result.Status.Authenticated {
		return auth.User{}, fmt.Errorf("the API server did not authenticate the ServiceAccount token: %s", result.Status.Error)
	}
	// An authenticator that ignores audiences answers with none: the token is
	// then not shown to be meant for this audience.
	if !slices.Contains(result.Status.Audiences, a.tokenAudience) {
		return auth.User{}, fmt.Errorf("the API server did not authenticate the ServiceAccount token for the audience %q", a.tokenAudience)
	}
	if !strings.HasPrefix(result.Status.User.Username, serviceAccountPrefix) {
		return auth.User{}, fmt.Errorf("the token authenticates %q, not a ServiceAccount", result.Status.User.Username)
	}
	return auth.User{ID: result.Status.User.Username, Groups: result.Status.User.Groups, Kubernetes: true}, nil
}

// userIDFromRequest returns the user identity from the user_id query param or
// X-User-Id header, preferring the query param.
func userIDFromRequest(headers http.Header, query url.Values) string {
	if v := query.Get("user_id"); v != "" {
		return v
	}
	return headers.Get("X-User-Id")
}

func (a *ProxyAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	if simpleSession, ok := session.(*SimpleSession); ok {
		if simpleSession.authHeader != "" {
			r.Header.Set("Authorization", simpleSession.authHeader)
		}
		if userID := simpleSession.P.User.ID; userID != "" {
			r.Header.Set("X-User-Id", userID)
			// The groups go with the user ID, so that the calls the agent
			// makes for the user are reviewed with the same groups.
			if groups := auth.EncodeUserGroups(simpleSession.P.User.Groups); groups != "" {
				r.Header.Set(auth.UserGroupsHeader, groups)
			} else {
				r.Header.Del(auth.UserGroupsHeader)
			}
		}
	}
	return nil
}

// groupsFromClaim reads a groups claim the way the API server does: absent is
// no groups, a string is one group, and a list holds only strings.
func groupsFromClaim(value any, claim string) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{typed}, nil
	case []any:
		groups := make([]string, 0, len(typed))
		for _, item := range typed {
			group, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("the %q claim holds a group that is not a string", claim)
			}
			groups = append(groups, group)
		}
		return groups, nil
	default:
		return nil, fmt.Errorf("the %q claim is neither a string nor a list", claim)
	}
}

// parseJWTPayload decodes JWT payload without signature verification
func parseJWTPayload(tokenString string) (map[string]any, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}

	return claims, nil
}
