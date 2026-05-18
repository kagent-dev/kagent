package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const externalBearerMaxIntrospectionBodyBytes int64 = 64 * 1024

type ExternalBearerAuthenticatorConfig struct {
	URL                               string
	Timeout                           time.Duration
	PropagateToken                    bool
	ValidationAuthorization           string
	ClientID                          string
	ClientSecret                      string
	AllowUnauthenticatedIntrospection bool
	UserIDClaim                       string
	HTTPClient                        *http.Client
}

type ExternalBearerAuthenticator struct {
	introspectionURL        string
	client                  *http.Client
	propagateToken          bool
	validationAuthorization string
	clientID                string
	clientSecret            string
	userIDClaim             string
}

type externalBearerSession struct {
	P           auth.Principal
	bearerToken string
	expiresAt   *time.Time
}

func (s *externalBearerSession) Principal() auth.Principal {
	return s.P
}

func NewExternalBearerAuthenticator(cfg ExternalBearerAuthenticatorConfig) (*ExternalBearerAuthenticator, error) {
	if cfg.URL == "" {
		return nil, errors.New("external-bearer auth requires AUTH_EXTERNAL_BEARER_URL")
	}
	if err := validateIntrospectionURL(cfg.URL); err != nil {
		return nil, err
	}
	if cfg.ValidationAuthorization != "" && (cfg.ClientID != "" || cfg.ClientSecret != "") {
		return nil, errors.New("external-bearer auth config cannot set ValidationAuthorization with ClientID/ClientSecret")
	}
	if (cfg.ClientID == "") != (cfg.ClientSecret == "") {
		return nil, errors.New("external-bearer auth config requires both ClientID and ClientSecret for Basic introspection auth")
	}
	if cfg.ValidationAuthorization == "" && cfg.ClientID == "" && !cfg.AllowUnauthenticatedIntrospection {
		return nil, errors.New("external-bearer auth config requires introspection endpoint authentication or AllowUnauthenticatedIntrospection for local/test use")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	} else {
		copy := *client
		client = &copy
	}
	if client.Timeout == 0 {
		client.Timeout = timeout
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	userIDClaim := cfg.UserIDClaim
	if userIDClaim == "" {
		userIDClaim = "sub"
	}

	return &ExternalBearerAuthenticator{
		introspectionURL:        cfg.URL,
		client:                  client,
		propagateToken:          cfg.PropagateToken,
		validationAuthorization: cfg.ValidationAuthorization,
		clientID:                cfg.ClientID,
		clientSecret:            cfg.ClientSecret,
		userIDClaim:             userIDClaim,
	}, nil
}

func (a *ExternalBearerAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (auth.Session, error) {
	token, ok := bearerTokenFromAuthorization(reqHeaders.Get("Authorization"))
	if !ok {
		return nil, ErrUnauthenticated
	}

	claims, expiresAt, err := a.introspect(ctx, token)
	if err != nil {
		return nil, ErrUnauthenticated
	}

	if isServiceTokenClaims(claims) {
		// Service actor policy/classification is handled in a later slice. Until then,
		// positively identified service-token claims must not fall through to human
		// user propagation via username/sub fallbacks.
		return nil, ErrUnauthenticated
	}

	userID := identityFromClaims(claims, a.userIDClaim)
	if userID == "" {
		return nil, ErrUnauthenticated
	}

	return &externalBearerSession{
		P: auth.Principal{
			User:   auth.User{ID: userID},
			Claims: claimsWithoutRawBearerToken(claims, token),
		},
		bearerToken: token,
		expiresAt:   expiresAt,
	}, nil
}

func (a *ExternalBearerAuthenticator) UpstreamAuth(r *http.Request, session auth.Session, upstreamPrincipal auth.Principal) error {
	if session == nil {
		return nil
	}
	if externalSession, ok := session.(*externalBearerSession); ok {
		if externalSession.expiresAt != nil && !externalSession.expiresAt.After(time.Now()) {
			return ErrUnauthenticated
		}
		r.Header.Del("Authorization")
		if a.propagateToken && externalSession.bearerToken != "" {
			r.Header.Set("Authorization", "Bearer "+externalSession.bearerToken)
		}
	}
	principal := session.Principal()
	if principal.User.ID != "" {
		r.Header.Set("X-User-Id", principal.User.ID)
	}
	return nil
}

func (a *ExternalBearerAuthenticator) introspect(ctx context.Context, token string) (map[string]any, *time.Time, error) {
	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.introspectionURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if a.validationAuthorization != "" {
		req.Header.Set("Authorization", a.validationAuthorization)
	} else if a.clientID != "" {
		req.Header.Set("Authorization", "Basic "+basicAuth(a.clientID, a.clientSecret))
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := readBounded(resp.Body, externalBearerMaxIntrospectionBodyBytes)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, nil, fmt.Errorf("introspection endpoint returned status %d", resp.StatusCode)
	}
	if !isJSONContentType(resp.Header.Get("Content-Type")) {
		return nil, nil, fmt.Errorf("introspection endpoint returned unsupported Content-Type %q", resp.Header.Get("Content-Type"))
	}

	var claims map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(body), &claims); err != nil {
		return nil, nil, err
	}
	if len(claims) == 0 {
		return nil, nil, errors.New("empty introspection response")
	}
	active, ok := claims["active"].(bool)
	if !ok || !active {
		return nil, nil, errors.New("token is inactive")
	}

	expiresAt, err := expiryFromClaims(claims)
	if err != nil {
		return nil, nil, err
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, nil, errors.New("token is expired")
	}

	return claims, expiresAt, nil
}

func validateIntrospectionURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid external-bearer introspection URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("external-bearer introspection URL must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("external-bearer introspection URL must include a host")
	}
	if parsed.Scheme == "http" && !isLocalhost(parsed.Hostname()) {
		return fmt.Errorf("external-bearer introspection URL must use https for non-localhost hosts")
	}
	return nil
}

func isLocalhost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("introspection response body exceeds %d bytes", max)
	}
	return body, nil
}

func basicAuth(clientID, clientSecret string) string {
	return base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
}

func bearerTokenFromAuthorization(authHeader string) (string, bool) {
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func isJSONContentType(contentType string) bool {
	if strings.TrimSpace(contentType) == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json")
}

func isServiceTokenClaims(claims map[string]any) bool {
	grantType, _ := claims["grant_type"].(string)
	return strings.EqualFold(grantType, "client_credentials")
}

func claimsWithoutRawBearerToken(claims map[string]any, token string) map[string]any {
	if token == "" {
		return claims
	}
	sanitized := make(map[string]any, len(claims))
	for key, value := range claims {
		if sanitizedValue, ok := sanitizeClaimValue(value, token); ok {
			sanitized[key] = sanitizedValue
		}
	}
	return sanitized
}

func sanitizeClaimValue(value any, token string) (any, bool) {
	switch typed := value.(type) {
	case string:
		if strings.Contains(typed, token) {
			return nil, false
		}
		return typed, true
	case []any:
		sanitized := make([]any, 0, len(typed))
		for _, item := range typed {
			if sanitizedItem, ok := sanitizeClaimValue(item, token); ok {
				sanitized = append(sanitized, sanitizedItem)
			}
		}
		return sanitized, true
	case map[string]any:
		sanitized := make(map[string]any, len(typed))
		for key, item := range typed {
			if sanitizedItem, ok := sanitizeClaimValue(item, token); ok {
				sanitized[key] = sanitizedItem
			}
		}
		return sanitized, true
	default:
		return value, true
	}
}

func identityFromClaims(claims map[string]any, userIDClaim string) string {
	for _, claim := range []string{"username", userIDClaim, "sub", "subject"} {
		if claim == "" {
			continue
		}
		if value, ok := claims[claim].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func expiryFromClaims(claims map[string]any) (*time.Time, error) {
	raw, ok := claims["exp"]
	if !ok || raw == nil {
		return nil, nil
	}
	var unix int64
	switch v := raw.(type) {
	case float64:
		unix = int64(v)
		if v != float64(unix) {
			return nil, errors.New("exp must be a unix timestamp")
		}
	case int64:
		unix = v
	case int:
		unix = int64(v)
	case json.Number:
		parsed, err := strconv.ParseInt(v.String(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid exp claim: %w", err)
		}
		unix = parsed
	default:
		return nil, errors.New("exp must be a unix timestamp")
	}
	expiresAt := time.Unix(unix, 0)
	return &expiresAt, nil
}

var _ auth.AuthProvider = (*ExternalBearerAuthenticator)(nil)
