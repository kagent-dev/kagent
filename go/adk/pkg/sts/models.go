// Package sts implements OAuth 2.0 Token Exchange (RFC 8693) for the Go ADK.
// This package provides a Security Token Service (STS) client with Kubernetes
// service account token support and ADK plugin integration for token propagation.
package sts

// TokenType represents RFC 8693 defined token types.
type TokenType string

const (
	// TokenTypeJWT is the JWT token type
	TokenTypeJWT TokenType = "urn:ietf:params:oauth:token-type:jwt"
	// TokenTypeSAML2 is the SAML2 token type
	TokenTypeSAML2 TokenType = "urn:ietf:params:oauth:token-type:saml2"
	// TokenTypeSAML1 is the SAML1 token type
	TokenTypeSAML1 TokenType = "urn:ietf:params:oauth:token-type:saml1"
	// TokenTypeIDToken is the ID token type
	TokenTypeIDToken TokenType = "urn:ietf:params:oauth:token-type:id_token"
	// TokenTypeAccessToken is the access token type
	TokenTypeAccessToken TokenType = "urn:ietf:params:oauth:token-type:access_token"
)

// GrantType represents OAuth 2.0 grant types.
type GrantType string

const (
	// GrantTypeTokenExchange is the RFC 8693 token exchange grant type
	GrantTypeTokenExchange GrantType = "urn:ietf:params:oauth:grant-type:token-exchange"
)

// TokenExchangeRequest represents an RFC 8693 Token Exchange Request.
type TokenExchangeRequest struct {
	// GrantType is the OAuth 2.0 grant type (required)
	GrantType GrantType `json:"grant_type"`
	// SubjectToken is the security token representing the identity of the party
	// on behalf of whom the new token is being requested (required)
	SubjectToken string `json:"subject_token"`
	// SubjectTokenType is the type of the subject_token (required)
	SubjectTokenType TokenType `json:"subject_token_type"`
	// ActorToken is the security token representing the identity of the acting party (optional)
	ActorToken string `json:"actor_token,omitempty"`
	// ActorTokenType is the type of the actor_token (required if ActorToken is set)
	ActorTokenType TokenType `json:"actor_token_type,omitempty"`
	// Resource is the logical name of the target service or resource (optional)
	Resource any `json:"resource,omitempty"` // Can be string or []string
	// Audience is the logical name of the target service or resource (optional)
	Audience any `json:"audience,omitempty"` // Can be string or []string
	// Scope is the scope of the requested token (optional)
	Scope string `json:"scope,omitempty"`
	// RequestedTokenType is the type of the requested token (optional)
	RequestedTokenType TokenType `json:"requested_token_type,omitempty"`
	// AdditionalParameters contains additional parameters for the request (optional)
	AdditionalParameters map[string]any `json:"-"` // Not serialized directly, merged into form data
}

// IsDelegationRequest checks if this is a delegation request (has actor_token).
func (r *TokenExchangeRequest) IsDelegationRequest() bool {
	return r.ActorToken != ""
}

// IsImpersonationRequest checks if this is an impersonation request (no actor_token).
func (r *TokenExchangeRequest) IsImpersonationRequest() bool {
	return r.ActorToken == ""
}

// TokenExchangeResponse represents an RFC 8693 Token Exchange Response.
type TokenExchangeResponse struct {
	// AccessToken is the issued security token (required)
	AccessToken string `json:"access_token"`
	// IssuedTokenType is the type of the issued token (required)
	IssuedTokenType TokenType `json:"issued_token_type"`
	// TokenType is the type of the access token (default: Bearer)
	TokenType string `json:"token_type,omitempty"`
	// ExpiresIn is the lifetime in seconds of the access token (optional)
	ExpiresIn int `json:"expires_in,omitempty"`
	// Scope is the scope of the access token (optional)
	Scope string `json:"scope,omitempty"`
	// RefreshToken is the refresh token if applicable (optional)
	RefreshToken string `json:"refresh_token,omitempty"`
	// AdditionalParameters contains additional response parameters (optional)
	AdditionalParameters map[string]any `json:"-"`
}

// TokenExchangeErrorResponse represents an RFC 8693 Token Exchange Error response.
type TokenExchangeErrorResponse struct {
	// Error is the error code (required)
	Error string `json:"error"`
	// ErrorDescription is a human-readable error description (optional)
	ErrorDescription string `json:"error_description,omitempty"`
	// ErrorURI is a URI identifying the error (optional)
	ErrorURI string `json:"error_uri,omitempty"`
	// AdditionalParameters contains additional error parameters (optional)
	AdditionalParameters map[string]any `json:"-"`
}

// WellKnownConfiguration represents OAuth 2.0 Authorization Server Metadata.
type WellKnownConfiguration struct {
	// Issuer is the authorization server's issuer identifier (required)
	Issuer string `json:"issuer"`
	// TokenEndpoint is the token endpoint URL (required)
	TokenEndpoint string `json:"token_endpoint"`
	// TokenEndpointAuthMethodsSupported is the list of supported auth methods (optional)
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	// TokenEndpointAuthSigningAlgValuesSupported is the list of supported signing algorithms (optional)
	TokenEndpointAuthSigningAlgValuesSupported []string `json:"token_endpoint_auth_signing_alg_values_supported,omitempty"`
	// AdditionalParameters contains additional configuration parameters (optional)
	AdditionalParameters map[string]any `json:"-"`
}

// STSConfig holds configuration for the STS client.
type STSConfig struct {
	// WellKnownURI is the well-known configuration URI (required)
	WellKnownURI string
	// Timeout is the request timeout in seconds (default: 5)
	Timeout int
	// VerifySSL controls whether to verify SSL certificates (default: true)
	VerifySSL *bool
	// UseIssuerHost replaces the host:port in token_endpoint with the host:port from well_known_uri
	UseIssuerHost bool
}

// DefaultSTSConfig returns a default STS configuration.
func DefaultSTSConfig(wellKnownURI string) STSConfig {
	verifySSL := true
	return STSConfig{
		WellKnownURI:  wellKnownURI,
		Timeout:       5,
		VerifySSL:     &verifySSL,
		UseIssuerHost: false,
	}
}
