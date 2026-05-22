package sts

import (
	"context"
	"fmt"
	"sync"
)

// GetSubjectTokenFunc is a function type for extracting subject tokens.
// It receives the bearer token (from Authorization header) and should return
// the subject token for STS exchange, or empty string if not available.
type GetSubjectTokenFunc func(bearerToken string) string

// DefaultGetSubjectToken extracts the JWT token from the Authorization header.
// It expects the bearerToken to already be the JWT (without "Bearer " prefix).
// This matches how executor.go stores the token in context.
func DefaultGetSubjectToken(bearerToken string) string {
	return bearerToken
}

// FetchActorTokenFunc is a function type for fetching actor tokens dynamically.
// This can be used for scenarios where the actor token needs to be fetched
// at runtime rather than being a static Kubernetes service account token.
type FetchActorTokenFunc func(ctx context.Context) (string, error)

// STSIntegration provides framework-agnostic STS integration.
// It wires together the STS client, actor token service, and subject token extraction.
type STSIntegration struct {
	client            *STSClient
	actorTokenService *ActorTokenService
	fetchActorToken   FetchActorTokenFunc
	getSubjectToken   GetSubjectTokenFunc
	staticActorToken  string // cached static actor token from service
	actorTokenMu      sync.Mutex
}

// NewSTSIntegration creates a new STS integration.
//
// Parameters:
//   - wellKnownURI: The well-known configuration URI for the STS server
//   - serviceAccountTokenPath: Path to K8s service account token (ignored if fetchActorToken is set)
//   - fetchActorToken: Optional function to fetch actor token dynamically
//   - getSubjectToken: Optional function to extract subject token from context
//   - timeout: Request timeout in seconds (non-positive uses default: 5)
//   - verifySSL: Whether to verify SSL certificates (default: true)
//   - useIssuerHost: Replace host:port in token_endpoint with host:port from well_known_uri
//
// NOTE: If fetchActorToken is provided, serviceAccountTokenPath is ignored.
// If getSubjectToken is not provided, DefaultGetSubjectToken is used.
func NewSTSIntegration(
	wellKnownURI string,
	serviceAccountTokenPath string,
	fetchActorToken FetchActorTokenFunc,
	getSubjectToken GetSubjectTokenFunc,
	timeout int,
	verifySSL bool,
	useIssuerHost bool,
) (*STSIntegration, error) {
	config := STSConfig{
		WellKnownURI:  wellKnownURI,
		Timeout:       timeout,
		VerifySSL:     &verifySSL,
		UseIssuerHost: useIssuerHost,
	}

	integration := &STSIntegration{
		client:          NewSTSClient(config),
		fetchActorToken: fetchActorToken,
		getSubjectToken: getSubjectToken,
	}

	// Only set up actor token service if no dynamic fetch function provided
	if fetchActorToken == nil {
		integration.actorTokenService = NewActorTokenService(serviceAccountTokenPath)
	}

	// Use default subject token extraction if not provided
	if integration.getSubjectToken == nil {
		integration.getSubjectToken = DefaultGetSubjectToken
	}

	return integration, nil
}

// GetSubjectToken extracts the subject token from the bearer token.
func (i *STSIntegration) GetSubjectToken(bearerToken string) string {
	return i.getSubjectToken(bearerToken)
}

// getActorToken retrieves the actor token, either from cache, dynamic fetch, or file.
func (i *STSIntegration) getActorToken(ctx context.Context) (string, error) {
	// Use dynamic fetch if provided
	if i.fetchActorToken != nil {
		return i.fetchActorToken(ctx)
	}

	i.actorTokenMu.Lock()
	defer i.actorTokenMu.Unlock()

	// Use cached static token if available
	if i.staticActorToken != "" {
		return i.staticActorToken, nil
	}

	// Load from service account token file (one-time load for static tokens)
	if i.actorTokenService != nil {
		token, err := i.actorTokenService.GetActorToken()
		if err != nil {
			return "", fmt.Errorf("failed to get actor token from service: %w", err)
		}
		i.staticActorToken = token
		return token, nil
	}

	return "", nil
}

func (i *STSIntegration) actorTokenForExchange(ctx context.Context) (string, error) {
	actorToken, err := i.getActorToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch actor token: %w", err)
	}
	if actorToken == "" {
		return "", nil
	}
	return actorToken, nil
}

// ExchangeToken performs a token exchange using the STS client.
// It automatically handles actor token retrieval when needed.
func (i *STSIntegration) ExchangeToken(
	ctx context.Context,
	subjectToken string,
	subjectTokenType TokenType,
	resource any,
	audience any,
	scope string,
	requestedTokenType TokenType,
) (*TokenExchangeResponse, error) {
	actorToken, err := i.actorTokenForExchange(ctx)
	if err != nil {
		return nil, err
	}

	return i.ExchangeTokenWithActorToken(ctx, subjectToken, subjectTokenType, actorToken, resource, audience, scope, requestedTokenType)
}

// ExchangeTokenWithActorToken performs a token exchange using an actor token
// selected by the caller. This lets ADK-specific integrations own dynamic
// actor-token caching while keeping raw STS client calls behind STSIntegration.
func (i *STSIntegration) ExchangeTokenWithActorToken(
	ctx context.Context,
	subjectToken string,
	subjectTokenType TokenType,
	actorToken string,
	resource any,
	audience any,
	scope string,
	requestedTokenType TokenType,
) (*TokenExchangeResponse, error) {
	switch actorToken {
	case "":
		return i.client.Impersonate(
			ctx,
			subjectToken,
			subjectTokenType,
			resource,
			audience,
			scope,
			requestedTokenType,
			nil,
		)
	default:
		return i.client.Delegate(
			ctx,
			subjectToken,
			subjectTokenType,
			actorToken,
			TokenTypeJWT,
			resource,
			audience,
			scope,
			requestedTokenType,
			nil,
		)
	}
}

// Client returns the underlying STS client for advanced use cases.
func (i *STSIntegration) Client() *STSClient {
	return i.client
}
