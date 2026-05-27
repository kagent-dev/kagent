package sts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// STSClient implements a Security Token Service client for RFC 8693 OAuth 2.0 Token Exchange.
type STSClient struct {
	config          STSConfig
	wellKnownConfig *WellKnownConfiguration
	httpClient      *http.Client
	initMu          sync.Mutex
}

// NewSTSClient creates a new STS client with the given configuration.
func NewSTSClient(config STSConfig) *STSClient {
	defaults := DefaultSTSConfig(config.WellKnownURI)
	if config.Timeout == 0 {
		config.Timeout = defaults.Timeout
	}
	if config.VerifySSL == nil {
		config.VerifySSL = defaults.VerifySSL
	}
	return &STSClient{
		config: config,
	}
}

// initialize performs lazy initialization of the client.
// Fetches well-known configuration if not already cached.
func (c *STSClient) initialize(ctx context.Context) error {
	c.initMu.Lock()
	defer c.initMu.Unlock()

	if c.wellKnownConfig != nil && c.httpClient != nil {
		return nil
	}

	if c.wellKnownConfig == nil {
		wellKnownConfig, err := FetchWellKnownConfiguration(
			ctx,
			c.config.WellKnownURI,
			c.config.Timeout,
			*c.config.VerifySSL,
			c.config.UseIssuerHost,
		)
		if err != nil {
			return err
		}
		c.wellKnownConfig = wellKnownConfig
	}

	if c.httpClient == nil {
		c.httpClient = &http.Client{
			Timeout:   time.Duration(c.config.Timeout) * time.Second,
			Transport: transportWithTLSVerification(*c.config.VerifySSL),
		}
	}

	return nil
}

// buildFormData creates the form-encoded request data from a TokenExchangeRequest.
func (c *STSClient) buildFormData(req *TokenExchangeRequest) url.Values {
	data := url.Values{}
	data.Set("grant_type", string(req.GrantType))
	data.Set("subject_token", req.SubjectToken)
	data.Set("subject_token_type", string(req.SubjectTokenType))

	// Add actor token for delegation requests
	if req.ActorToken != "" {
		data.Set("actor_token", req.ActorToken)
		if req.ActorTokenType != "" {
			data.Set("actor_token_type", string(req.ActorTokenType))
		}
	}

	// Add optional parameters
	if req.Resource != nil {
		switch v := req.Resource.(type) {
		case string:
			data.Set("resource", v)
		case []string:
			for _, r := range v {
				data.Add("resource", r)
			}
		}
	}

	if req.Audience != nil {
		switch v := req.Audience.(type) {
		case string:
			data.Set("audience", v)
		case []string:
			for _, a := range v {
				data.Add("audience", a)
			}
		}
	}

	if req.Scope != "" {
		data.Set("scope", req.Scope)
	}

	if req.RequestedTokenType != "" {
		data.Set("requested_token_type", string(req.RequestedTokenType))
	}

	// Add additional parameters
	if req.AdditionalParameters != nil {
		for key, value := range req.AdditionalParameters {
			if v, ok := value.(string); ok {
				data.Set(key, v)
			}
		}
	}

	return data
}

// ExchangeToken performs a token exchange using RFC 8693 OAuth 2.0 Token Exchange.
//
// NOTE: The actor_token and actor_token_type parameters enable delegation scenarios.
// For impersonation (no delegation), omit these parameters.
func (c *STSClient) ExchangeToken(
	ctx context.Context,
	subjectToken string,
	subjectTokenType TokenType,
	actorToken string,
	actorTokenType TokenType,
	resource any,
	audience any,
	scope string,
	requestedTokenType TokenType,
	additionalParameters map[string]any,
) (*TokenExchangeResponse, error) {
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}

	// Validate actor token type requirement
	if actorToken != "" && actorTokenType == "" {
		return nil, NewConfigurationError("actor_token_type is required when actor_token is provided")
	}

	req := &TokenExchangeRequest{
		GrantType:            GrantTypeTokenExchange,
		SubjectToken:         subjectToken,
		SubjectTokenType:     subjectTokenType,
		ActorToken:           actorToken,
		ActorTokenType:       actorTokenType,
		Resource:             resource,
		Audience:             audience,
		Scope:                scope,
		RequestedTokenType:   requestedTokenType,
		AdditionalParameters: additionalParameters,
	}

	formData := c.buildFormData(req)

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.wellKnownConfig.TokenEndpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, NewNetworkError("failed to create token exchange request", err)
	}

	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(postReq)
	if err != nil {
		return nil, NewNetworkError("network error during token exchange", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result TokenExchangeResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, NewConfigurationError(fmt.Sprintf("invalid token exchange response: %v", err))
		}
		return &result, nil
	}

	// Parse error response
	var responseData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		// Could not parse error as JSON
		return nil, NewTokenExchangeError(
			"invalid_response",
			fmt.Sprintf("Invalid error response from STS server: status %d", resp.StatusCode),
			resp.StatusCode,
		)
	}

	// Extract error details
	errorCode := "unknown_error"
	if ec, ok := responseData["error"].(string); ok {
		errorCode = ec
	}

	errorDescription := ""
	if ed, ok := responseData["error_description"].(string); ok {
		errorDescription = ed
	}

	return nil, NewTokenExchangeError(errorCode, errorDescription, resp.StatusCode)
}

// Impersonate performs an impersonation token exchange (no actor token).
func (c *STSClient) Impersonate(
	ctx context.Context,
	subjectToken string,
	subjectTokenType TokenType,
	resource any,
	audience any,
	scope string,
	requestedTokenType TokenType,
	additionalParameters map[string]any,
) (*TokenExchangeResponse, error) {
	return c.ExchangeToken(
		ctx,
		subjectToken,
		subjectTokenType,
		"", // no actor token
		"", // no actor token type
		resource,
		audience,
		scope,
		requestedTokenType,
		additionalParameters,
	)
}

// Delegate performs a delegation token exchange (with actor token).
func (c *STSClient) Delegate(
	ctx context.Context,
	subjectToken string,
	subjectTokenType TokenType,
	actorToken string,
	actorTokenType TokenType,
	resource any,
	audience any,
	scope string,
	requestedTokenType TokenType,
	additionalParameters map[string]any,
) (*TokenExchangeResponse, error) {
	if subjectToken == "" {
		return nil, NewAuthenticationError("subject token required for delegation")
	}
	if actorToken == "" {
		return nil, NewAuthenticationError("actor token required for delegation")
	}
	return c.ExchangeToken(
		ctx,
		subjectToken,
		subjectTokenType,
		actorToken,
		actorTokenType,
		resource,
		audience,
		scope,
		requestedTokenType,
		additionalParameters,
	)
}
