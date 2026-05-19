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
	"os"
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
	PolicyFile                        string
}

type ExternalBearerAuthenticator struct {
	introspectionURL        string
	client                  *http.Client
	propagateToken          bool
	validationAuthorization string
	clientID                string
	clientSecret            string
	userIDClaim             string
	policy                  *externalBearerPolicy
}

type externalBearerSession struct {
	P              auth.Principal
	bearerToken    string
	expiresAt      *time.Time
	actorType      externalBearerActorType
	serviceActorID string
}

type externalBearerActorType string

const (
	externalBearerActorUser    externalBearerActorType = "user"
	externalBearerActorService externalBearerActorType = "service"
)

func (s *externalBearerSession) Principal() auth.Principal {
	return s.P
}

func (s *externalBearerSession) A2AOnly() bool {
	return s.actorType == externalBearerActorService
}

func NewExternalBearerAuthenticator(cfg ExternalBearerAuthenticatorConfig) (*ExternalBearerAuthenticator, error) {
	if cfg.URL == "" {
		return nil, errors.New("external-bearer auth requires AUTH_EXTERNAL_BEARER_URL")
	}
	if err := validateIntrospectionURL(cfg.URL); err != nil {
		return nil, err
	}
	if cfg.AllowUnauthenticatedIntrospection {
		parsed, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid external-bearer introspection URL: %w", err)
		}
		if !isLocalhost(parsed.Hostname()) {
			return nil, errors.New("external-bearer auth config AllowUnauthenticatedIntrospection is only allowed for localhost/loopback introspection URLs")
		}
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
	policy, err := loadExternalBearerPolicyFile(cfg.PolicyFile)
	if err != nil {
		return nil, err
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
		policy:                  policy,
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

	if a.policy != nil {
		// Top-level policy checks are applied globally in this implementation:
		// user and service-actor tokens must both satisfy configured scopes,
		// audiences, and issuers before kagent accepts the session.
		if err := a.policy.checkTopLevelClaims(claims); err != nil {
			return nil, ErrUnauthenticated
		}
		serviceActorID, ok, err := a.policy.matchServiceActor(claims)
		if err != nil {
			return nil, ErrUnauthenticated
		}
		if ok {
			return &externalBearerSession{
				P: auth.Principal{
					Claims: claimsWithoutRawBearerToken(claims, token),
				},
				bearerToken:    token,
				expiresAt:      expiresAt,
				actorType:      externalBearerActorService,
				serviceActorID: serviceActorID,
			}, nil
		}
	}

	if isServiceTokenClaims(claims) || (a.policy != nil && a.policy.matchesServiceTokenIndicator(claims)) {
		// Positively identified service/client-credentials tokens require an explicit
		// serviceActors policy match and must not fall through to human user auth.
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
		actorType:   externalBearerActorUser,
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

func (a *ExternalBearerAuthenticator) CheckA2AAccess(ctx context.Context, session auth.Session, target auth.A2ATarget) error {
	_ = ctx
	externalSession, ok := session.(*externalBearerSession)
	if !ok {
		return errors.New("external-bearer A2A access requires an external-bearer session")
	}
	if externalSession.actorType != externalBearerActorService {
		return nil
	}
	if a.policy == nil || externalSession.serviceActorID == "" {
		return errors.New("external-bearer service actor has no A2A policy")
	}
	serviceActor, ok := a.policy.ServiceActors[externalSession.serviceActorID]
	if !ok {
		return errors.New("external-bearer service actor policy not found")
	}
	for _, allowed := range serviceActor.AllowedA2A {
		if allowed.matches(target) {
			return nil
		}
	}
	return fmt.Errorf("external-bearer service actor %q is not allowed to access A2A target %s/%s (%s)", externalSession.serviceActorID, target.Namespace, target.Name, target.WorkloadType)
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

type externalBearerPolicy struct {
	RequiredScopes   []string                              `json:"requiredScopes"`
	AllowedAudiences []string                              `json:"allowedAudiences"`
	AllowedIssuers   []string                              `json:"allowedIssuers"`
	ServiceActors    map[string]externalBearerServiceActor `json:"serviceActors"`
}

type externalBearerServiceActor struct {
	Match      externalBearerMatch       `json:"match"`
	AllowedA2A []externalBearerA2ATarget `json:"allowedA2A"`
}

type externalBearerMatch struct {
	AllOf []externalBearerPredicate `json:"allOf"`
}

type externalBearerPredicate struct {
	Claim    string
	Value    *string
	Contains *string
}

type externalBearerA2ATarget struct {
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	WorkloadType string `json:"workloadType"`
}

func (p *externalBearerPredicate) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range raw {
		switch key {
		case "claim", "value", "contains":
		default:
			return fmt.Errorf("unknown external-bearer policy predicate operator or field %q", key)
		}
	}
	if rawClaim, ok := raw["claim"]; ok {
		if err := json.Unmarshal(rawClaim, &p.Claim); err != nil {
			return fmt.Errorf("invalid external-bearer policy predicate claim: %w", err)
		}
	}
	if rawValue, ok := raw["value"]; ok {
		var value string
		if err := json.Unmarshal(rawValue, &value); err != nil {
			return fmt.Errorf("invalid external-bearer policy predicate value: %w", err)
		}
		p.Value = &value
	}
	if rawContains, ok := raw["contains"]; ok {
		var contains string
		if err := json.Unmarshal(rawContains, &contains); err != nil {
			return fmt.Errorf("invalid external-bearer policy predicate contains: %w", err)
		}
		p.Contains = &contains
	}
	return nil
}

func loadExternalBearerPolicyFile(path string) (*externalBearerPolicy, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read external-bearer policy file %q: %w", path, err)
	}
	var policy externalBearerPolicy
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return nil, fmt.Errorf("invalid external-bearer policy file %q: %w", path, err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("invalid external-bearer policy file %q: trailing JSON after policy object", path)
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("invalid external-bearer policy file %q: %w", path, err)
		}
		return nil, fmt.Errorf("invalid external-bearer policy file %q: trailing JSON after policy object near %v", path, token)
	}
	if policy.ServiceActors == nil {
		policy.ServiceActors = map[string]externalBearerServiceActor{}
	}
	if err := policy.validate(); err != nil {
		return nil, fmt.Errorf("invalid external-bearer policy file %q: %w", path, err)
	}
	return &policy, nil
}

func (p *externalBearerPolicy) validate() error {
	for actorID, serviceActor := range p.ServiceActors {
		if strings.TrimSpace(actorID) == "" {
			return errors.New("serviceActors contains empty actor id")
		}
		if len(serviceActor.Match.AllOf) == 0 {
			return fmt.Errorf("service actor %q match.allOf must contain at least one predicate", actorID)
		}
		if len(serviceActor.Match.AllOf) == 1 {
			if serviceActor.Match.AllOf[0].Claim == "client_id" {
				return fmt.Errorf("service actor %q match.allOf cannot classify by client_id alone", actorID)
			}
			return fmt.Errorf("service actor %q match.allOf must contain at least two predicates", actorID)
		}
		hasServiceTokenIndicator := false
		for i, predicate := range serviceActor.Match.AllOf {
			if err := predicate.validate(); err != nil {
				return fmt.Errorf("service actor %q match.allOf[%d]: %w", actorID, i, err)
			}
			if predicate.isServiceTokenIndicator() {
				hasServiceTokenIndicator = true
			}
		}
		if !hasServiceTokenIndicator {
			return fmt.Errorf("service actor %q match.allOf must include a recognizable service-token indicator predicate", actorID)
		}
		for i, target := range serviceActor.AllowedA2A {
			if err := target.validate(); err != nil {
				return fmt.Errorf("service actor %q allowedA2A[%d]: %w", actorID, i, err)
			}
		}
	}
	return nil
}

func (p externalBearerPredicate) validate() error {
	if strings.TrimSpace(p.Claim) == "" {
		return errors.New("claim is required")
	}
	operators := 0
	if p.Value != nil {
		if strings.TrimSpace(*p.Value) == "" {
			return errors.New("predicate value must not be empty")
		}
		operators++
	}
	if p.Contains != nil {
		if strings.TrimSpace(*p.Contains) == "" {
			return errors.New("predicate contains value must not be empty")
		}
		operators++
	}
	if operators != 1 {
		return errors.New("predicate must specify exactly one operator: value or contains")
	}
	return nil
}

func (t externalBearerA2ATarget) validate() error {
	if err := validatePolicyWildcardField("namespace", t.Namespace); err != nil {
		return err
	}
	if err := validatePolicyWildcardField("name", t.Name); err != nil {
		return err
	}
	if err := validatePolicyWildcardField("workloadType", t.WorkloadType); err != nil {
		return err
	}
	if t.WorkloadType != "agent" && t.WorkloadType != "sandbox" && t.WorkloadType != "*" {
		return fmt.Errorf("unknown workloadType %q", t.WorkloadType)
	}
	return nil
}

func validatePolicyWildcardField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.Contains(value, "*") && value != "*" {
		return fmt.Errorf("%s contains partial wildcard %q; only whole-field * is allowed", field, value)
	}
	return nil
}

func (p *externalBearerPolicy) checkTopLevelClaims(claims map[string]any) error {
	for _, requiredScope := range p.RequiredScopes {
		if !claimContains(claims["scope"], requiredScope, true) {
			return fmt.Errorf("required scope %q missing", requiredScope)
		}
	}
	if len(p.AllowedAudiences) > 0 && !anyClaimValueMatches(claims["aud"], p.AllowedAudiences) {
		return errors.New("aud claim missing or not allowed")
	}
	if len(p.AllowedIssuers) > 0 && !anyClaimValueMatches(claims["iss"], p.AllowedIssuers) {
		return errors.New("iss claim missing or not allowed")
	}
	return nil
}

func (p *externalBearerPolicy) matchesServiceTokenIndicator(claims map[string]any) bool {
	for _, serviceActor := range p.ServiceActors {
		for _, predicate := range serviceActor.Match.AllOf {
			if predicate.isServiceTokenIndicator() && predicate.matches(claims) {
				return true
			}
		}
	}
	return false
}

func (p *externalBearerPolicy) matchServiceActor(claims map[string]any) (string, bool, error) {
	var matchedActorID string
	for actorID, serviceActor := range p.ServiceActors {
		matched := true
		for _, predicate := range serviceActor.Match.AllOf {
			if !predicate.matches(claims) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if matchedActorID != "" {
			return "", false, fmt.Errorf("claims match multiple external-bearer service actors: %q and %q", matchedActorID, actorID)
		}
		matchedActorID = actorID
	}
	if matchedActorID == "" {
		return "", false, nil
	}
	return matchedActorID, true, nil
}

func (p externalBearerPredicate) isServiceTokenIndicator() bool {
	want, ok := p.expectedString()
	if !ok {
		return false
	}
	return isServiceTokenIndicatorClaim(p.Claim, want)
}

func (p externalBearerPredicate) expectedString() (string, bool) {
	switch {
	case p.Value != nil:
		return *p.Value, true
	case p.Contains != nil:
		return *p.Contains, true
	default:
		return "", false
	}
}

func (p externalBearerPredicate) matches(claims map[string]any) bool {
	value, ok := claims[p.Claim]
	if !ok {
		return false
	}
	if p.Value != nil {
		return claimValueEquals(value, *p.Value)
	}
	if p.Contains != nil {
		return claimContains(value, *p.Contains, p.Claim == "scope")
	}
	return false
}

func (t externalBearerA2ATarget) matches(target auth.A2ATarget) bool {
	return policyFieldMatches(t.Namespace, target.Namespace) &&
		policyFieldMatches(t.Name, target.Name) &&
		policyFieldMatches(t.WorkloadType, string(target.WorkloadType))
}

func policyFieldMatches(pattern, value string) bool {
	return pattern == "*" || pattern == value
}

func anyClaimValueMatches(value any, allowed []string) bool {
	for _, candidate := range allowed {
		if claimValueEquals(value, candidate) {
			return true
		}
	}
	return false
}

func claimValueEquals(value any, want string) bool {
	switch typed := value.(type) {
	case string:
		return typed == want
	case []any:
		for _, item := range typed {
			if itemString, ok := item.(string); ok && itemString == want {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == want {
				return true
			}
		}
	}
	return false
}

func claimContains(value any, want string, splitScopeString bool) bool {
	switch typed := value.(type) {
	case string:
		if splitScopeString {
			for _, token := range strings.Fields(typed) {
				if token == want {
					return true
				}
			}
			return false
		}
		return typed == want
	case []any:
		for _, item := range typed {
			if itemString, ok := item.(string); ok && itemString == want {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == want {
				return true
			}
		}
	}
	return false
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
	for _, claim := range []string{"grant_type", "token_class", "token_use"} {
		if claimHasServiceTokenIndicator(claim, claims[claim]) {
			return true
		}
	}
	return false
}

func claimHasServiceTokenIndicator(claim string, value any) bool {
	switch typed := value.(type) {
	case string:
		return isServiceTokenIndicatorClaim(claim, typed)
	case []any:
		for _, item := range typed {
			if claimHasServiceTokenIndicator(claim, item) {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if isServiceTokenIndicatorClaim(claim, item) {
				return true
			}
		}
	}
	return false
}

func isServiceTokenIndicatorClaim(claim, value string) bool {
	switch claim {
	case "grant_type":
		return strings.EqualFold(value, "client_credentials")
	case "token_class", "token_use":
		switch {
		case strings.EqualFold(value, "service"):
			return true
		case strings.EqualFold(value, "service_token"):
			return true
		case strings.EqualFold(value, "service-token"):
			return true
		case strings.EqualFold(value, "client_credentials"):
			return true
		}
	}
	return false
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
	seen := map[string]struct{}{}
	for _, claim := range []string{userIDClaim, "username", "sub", "subject"} {
		if claim == "" {
			continue
		}
		if _, ok := seen[claim]; ok {
			continue
		}
		seen[claim] = struct{}{}
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
var _ auth.A2AAccessProvider = (*ExternalBearerAuthenticator)(nil)
