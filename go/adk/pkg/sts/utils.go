package sts

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	httpProtocol  = "http://"
	httpsProtocol = "https://"
)

// FetchWellKnownConfiguration retrieves the OAuth 2.0 Authorization Server Metadata
// from the well-known configuration URI.
//
// NOTE: This makes an HTTP request. Callers should cache the result.
func FetchWellKnownConfiguration(ctx context.Context, wellKnownURI string, timeout int, verifySSL bool, useIssuerHost bool) (*WellKnownConfiguration, error) {
	client := &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: transportWithTLSVerification(verifySSL),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURI, nil)
	if err != nil {
		return nil, NewNetworkError("failed to create request", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, NewNetworkError("failed to fetch well-known configuration", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewNetworkError(fmt.Sprintf("failed to fetch well-known configuration: HTTP %d", resp.StatusCode), nil)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, NewConfigurationError(fmt.Sprintf("invalid well-known configuration response: %v", err))
	}

	// Add protocol to token_endpoint if it's missing
	if tokenEndpoint, ok := data["token_endpoint"].(string); ok {
		if !strings.HasPrefix(tokenEndpoint, httpProtocol) && !strings.HasPrefix(tokenEndpoint, httpsProtocol) {
			// Use the protocol from the well_known_uri
			protocol := httpProtocol
			if strings.HasPrefix(wellKnownURI, httpsProtocol) {
				protocol = httpsProtocol
			}
			data["token_endpoint"] = protocol + tokenEndpoint
		}

		// Replace host:port in token_endpoint with the host:port from well_known_uri
		// Protocol is already resolved above, so token_endpoint always has a scheme here
		if useIssuerHost {
			normalizedTokenEndpoint, _ := data["token_endpoint"].(string)
			issuer, err := url.Parse(wellKnownURI)
			if err != nil {
				return nil, NewConfigurationError(fmt.Sprintf("invalid well-known URI: %v", err))
			}

			endpoint, err := url.Parse(normalizedTokenEndpoint)
			if err != nil {
				return nil, NewConfigurationError(fmt.Sprintf("invalid token endpoint in configuration: %v", err))
			}

			// Replace netloc (host:port) with issuer's netloc
			newEndpoint := *endpoint
			newEndpoint.Host = issuer.Host
			data["token_endpoint"] = newEndpoint.String()
		}
	}

	config := &WellKnownConfiguration{
		Issuer:                            getString(data, "issuer"),
		TokenEndpoint:                     getString(data, "token_endpoint"),
		AdditionalParameters:              data,
		TokenEndpointAuthMethodsSupported: getStringSlice(data, "token_endpoint_auth_methods_supported"),
		TokenEndpointAuthSigningAlgValuesSupported: getStringSlice(data, "token_endpoint_auth_signing_alg_values_supported"),
	}

	// Validate required fields
	if config.Issuer == "" {
		return nil, NewConfigurationError("well-known configuration missing 'issuer' field")
	}
	if config.TokenEndpoint == "" {
		return nil, NewConfigurationError("well-known configuration missing 'token_endpoint' field")
	}

	return config, nil
}

// ParseTokenExchangeError parses a token exchange error response.
func ParseTokenExchangeError(responseData map[string]any) *TokenExchangeError {
	errorCode := "unknown_error"
	if ec, ok := responseData["error"].(string); ok {
		errorCode = ec
	}

	errorDescription := ""
	if ed, ok := responseData["error_description"].(string); ok {
		errorDescription = ed
	}

	return NewTokenExchangeError(errorCode, errorDescription, 0)
}

// Helper functions to safely extract values from map[string]any
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStringSlice(m map[string]any, key string) []string {
	if v, ok := m[key].([]any); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func transportWithTLSVerification(verifySSL bool) *http.Transport {
	transport := cloneDefaultHTTPTransport()
	if verifySSL {
		return transport
	}

	tlsConfig := &tls.Config{}
	if transport.TLSClientConfig != nil {
		tlsConfig = transport.TLSClientConfig.Clone()
	}
	tlsConfig.InsecureSkipVerify = true
	transport.TLSClientConfig = tlsConfig
	return transport
}

func cloneDefaultHTTPTransport() *http.Transport {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	return defaultTransport.Clone()
}
