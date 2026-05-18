package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type ExternalBearerAuthenticatorConfig struct {
	URL                     string
	Timeout                 time.Duration
	UserIDClaim             string
	ValidationAuthorization string
}

type ExternalBearerAuthenticator struct {
	url                     string
	timeout                 time.Duration
	userIDClaim             string
	validationAuthorization string
	client                  *http.Client
}

type externalBearerSession struct {
	principal      pkgauth.Principal
	actorType      string
	serviceActorID string
	expiresAt      int64
}

func (s *externalBearerSession) Principal() pkgauth.Principal {
	return s.principal
}

type externalBearerValidationRequest struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
}

type externalBearerValidationResponse struct {
	Active    bool           `json:"active"`
	Subject   string         `json:"subject"`
	UserID    string         `json:"user_id"`
	ActorType string         `json:"actor_type"`
	Claims    map[string]any `json:"claims"`
	ExpiresAt int64          `json:"expires_at"`
}

func NewExternalBearerAuthenticator(cfg ExternalBearerAuthenticatorConfig) (*ExternalBearerAuthenticator, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("external-bearer auth requires auth-external-bearer-url")
	}
	if _, err := url.ParseRequestURI(cfg.URL); err != nil {
		return nil, fmt.Errorf("invalid external-bearer validation URL: %w", err)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.UserIDClaim == "" {
		cfg.UserIDClaim = "sub"
	}
	return &ExternalBearerAuthenticator{
		url:                     cfg.URL,
		timeout:                 cfg.Timeout,
		userIDClaim:             cfg.UserIDClaim,
		validationAuthorization: cfg.ValidationAuthorization,
		client:                  &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (a *ExternalBearerAuthenticator) Authenticate(ctx context.Context, reqHeaders http.Header, query url.Values) (pkgauth.Session, error) {
	token, ok := strings.CutPrefix(reqHeaders.Get("Authorization"), "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return nil, ErrUnauthenticated
	}

	validation, err := a.validateToken(ctx, token)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if !validation.Active {
		return nil, ErrUnauthenticated
	}

	userID := validation.UserID
	if userID == "" && a.userIDClaim != "" && validation.Claims != nil {
		userID, _ = validation.Claims[a.userIDClaim].(string)
	}
	if userID == "" && validation.Claims != nil {
		userID, _ = validation.Claims["sub"].(string)
	}
	if userID == "" {
		userID = validation.Subject
	}
	if userID == "" {
		return nil, ErrUnauthenticated
	}

	actorType := validation.ActorType
	if actorType == "" {
		actorType = "user"
	}

	return &externalBearerSession{
		principal: pkgauth.Principal{
			User:   pkgauth.User{ID: userID},
			Claims: validation.Claims,
		},
		actorType:  actorType,
		expiresAt:  validation.ExpiresAt,
	}, nil
}

func (a *ExternalBearerAuthenticator) validateToken(ctx context.Context, token string) (*externalBearerValidationResponse, error) {
	body, err := json.Marshal(externalBearerValidationRequest{Token: token, TokenType: "Bearer"})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.validationAuthorization != "" {
		req.Header.Set("Authorization", a.validationAuthorization)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("external bearer validation returned status %d", resp.StatusCode)
	}

	var validation externalBearerValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		return nil, err
	}
	return &validation, nil
}

func (a *ExternalBearerAuthenticator) UpstreamAuth(r *http.Request, session pkgauth.Session, upstreamPrincipal pkgauth.Principal) error {
	if session == nil || session.Principal().User.ID == "" {
		return nil
	}
	r.Header.Set("X-User-Id", session.Principal().User.ID)

	return nil
}
