package models

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"
)

// defaultTimeout is the default execution timeout used by model implementations.
const defaultTimeout = 30 * time.Minute

// TransportConfig holds TLS, passthrough, and header settings shared by all model providers.
type TransportConfig struct {
	Headers               map[string]string
	TLSInsecureSkipVerify *bool
	TLSCACertPath         *string
	TLSDisableSystemCAs   *bool
	APIKeyPassthrough     bool
	Timeout               *int // seconds; nil = defaultTimeout
}

// BuildHTTPClient creates an http.Client with the full transport stack:
// TLS → passthrough auth → custom headers → timeout.
func BuildHTTPClient(tc TransportConfig) (*http.Client, error) {
	transport, err := BuildTLSTransport(
		http.DefaultTransport,
		tc.TLSInsecureSkipVerify,
		tc.TLSCACertPath,
		tc.TLSDisableSystemCAs,
	)
	if err != nil {
		return nil, err
	}

	if tc.APIKeyPassthrough {
		transport = &passthroughAuthTransport{base: transport}
	}

	if len(tc.Headers) > 0 {
		transport = &headerTransport{base: transport, headers: tc.Headers}
	}

	timeout := defaultTimeout
	if tc.Timeout != nil {
		timeout = time.Duration(*tc.Timeout) * time.Second
	}

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

// BearerTokenKey is the context key for storing the bearer token for API key passthrough
var BearerTokenKey = &contextKey{}

type contextKey struct{}

// passthroughAuthTransport wraps an http.RoundTripper and adds the Bearer token from context
type passthroughAuthTransport struct {
	base http.RoundTripper
}

func (t *passthroughAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, ok := req.Context().Value(BearerTokenKey).(string); ok && token != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return t.base.RoundTrip(req)
}

// headerTransport wraps an http.RoundTripper and adds custom headers to all requests
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// parametersJsonSchemaToMap converts a genai.FunctionDeclaration.ParametersJsonSchema value
// to map[string]any. ParametersJsonSchema is typed as `any` and can hold:
//   - map[string]any (rare — only if someone constructs it manually)
//   - *jsonschema.Schema (from functiontool.New and MCP tools via mcptoolset)
//   - any other JSON-serializable type
func parametersJsonSchemaToMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

// extractFunctionResponseContent converts a tool/function response value to a plain string:
//   - string: returned as-is
//   - map with "content" []any: all text items joined by newline (e.g. MCP tool responses)
//   - map with "result" string: returned directly
//   - anything else: JSON-marshalled
func extractFunctionResponseContent(resp any) string {
	if resp == nil {
		return ""
	}
	if s, ok := resp.(string); ok {
		return s
	}
	if m, ok := resp.(map[string]any); ok {
		// Content array (most common shape from MCP tools)
		if c, ok := m["content"].([]any); ok && len(c) > 0 {
			var parts []string
			for _, item := range c {
				if itemMap, ok := item.(map[string]any); ok {
					if t, ok := itemMap["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
		if r, ok := m["result"].(string); ok {
			return r
		}
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// genaiSchemaToMap converts a *genai.Schema to map[string]any.
// It JSON-marshals the full schema then lowercases all "type" values
func genaiSchemaToMap(s *genai.Schema) map[string]any {
	if s == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	lowercaseSchemaTypes(m)
	return m
}

// lowercaseSchemaTypes recursively lowercases all "type" values in a JSON Schema map.
// genai.Type constants are uppercase (e.g. "STRING") but JSON Schema requires lowercase.
func lowercaseSchemaTypes(m map[string]any) {
	if t, ok := m["type"].(string); ok {
		m["type"] = strings.ToLower(t)
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for _, v := range props {
			if sub, ok := v.(map[string]any); ok {
				lowercaseSchemaTypes(sub)
			}
		}
	}
	if items, ok := m["items"].(map[string]any); ok {
		lowercaseSchemaTypes(items)
	}
	if anyOf, ok := m["anyOf"].([]any); ok {
		for _, v := range anyOf {
			if sub, ok := v.(map[string]any); ok {
				lowercaseSchemaTypes(sub)
			}
		}
	}
}
