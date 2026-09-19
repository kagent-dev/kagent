package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/constants"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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
	Timeout               *int // seconds; nil = defaultTimeout. Bounds the whole request (connect + write + read).
	ConnectTimeout        *int // seconds; nil = Go transport default. Bounds connection establishment only.
}

// BuildHTTPClient creates an http.Client with the full transport stack:
// TLS → headers (the configured defaults, the runtime's identity, the user of
// the turn) → trace propagation → timeout.
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

	// Apply the connect timeout to the transport's dialer. Clone first so we
	// never mutate the shared http.DefaultTransport that BuildTLSTransport
	// returns when no TLS override is set.
	if tc.ConnectTimeout != nil {
		transport, err = withConnectTimeout(transport, time.Duration(*tc.ConnectTimeout)*time.Second)
		if err != nil {
			return nil, err
		}
	}

	transport = &headerTransport{base: transport, headers: tc.Headers, identity: runtimeIdentityHeaders()}

	// Outermost layer: inject W3C traceparent/tracestate from the active span so
	// LLM calls stay attached to the invocation trace instead of starting fresh
	// root traces at tracing-aware proxies (kagent-dev/kagent#2550).
	transport = otelhttp.NewTransport(transport)

	timeout := defaultTimeout
	if tc.Timeout != nil {
		timeout = time.Duration(*tc.Timeout) * time.Second
	}

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

// withConnectTimeout returns a copy of base (which must be an *http.Transport)
// whose DialContext enforces the given connection-establishment timeout. The
// base transport is cloned so callers can safely pass http.DefaultTransport
// without mutating the shared global.
func withConnectTimeout(base http.RoundTripper, connectTimeout time.Duration) (http.RoundTripper, error) {
	t, ok := base.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("BuildHTTPClient: connect timeout requires an *http.Transport base, got %T", base)
	}
	cloned := t.Clone()
	dialer := &net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}
	cloned.DialContext = dialer.DialContext
	return cloned, nil
}

// BearerTokenKey is the context key for storing the bearer token for API key passthrough
var BearerTokenKey = &contextKey{}

// PassthroughToken returns the caller's bearer token from ctx when apiKeyPassthrough
// is enabled, so every model/embedding provider resolves passthrough the same way.
// Each caller wraps the returned token in its own SDK's request-option type, since
// that varies by provider (e.g. Authorization vs Api-Key header).
//
// It reads BearerTokenKey alone and takes no call-context fallback, unlike
// BearerTokenFromContext: passthrough sends the credential to a third-party model
// provider, so it is limited to the contexts the executor threaded it through.
func PassthroughToken(ctx context.Context, apiKeyPassthrough bool) (token string, ok bool) {
	if !apiKeyPassthrough {
		return "", false
	}
	token, ok = ctx.Value(BearerTokenKey).(string)
	if !ok || token == "" {
		return "", false
	}
	return token, true
}

type contextKey struct{}

// BearerFromCallContext returns the bearer token carried by the A2A call
// context's Authorization header, or "" when there is none.
func BearerFromCallContext(ctx context.Context) string {
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return ""
	}
	meta := callCtx.ServiceParams()
	if meta == nil {
		return ""
	}
	vals, ok := meta.Get(constants.AuthorizationHeader)
	if !ok || len(vals) == 0 {
		return ""
	}
	parts := strings.Fields(strings.TrimSpace(vals[0]))
	if len(parts) >= 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

// BearerTokenFromContext returns the credential the request authenticates with.
// It prefers the value stored under BearerTokenKey and falls back to the A2A
// call context, which reaches callers whose context was not threaded through
// the executor.
func BearerTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(BearerTokenKey).(string); ok && token != "" {
		return token
	}
	return BearerFromCallContext(ctx)
}

// userKey is the context key of the authenticated user of a turn: its own
// type, so it can never be equal to another key. A pointer to a zero-size
// struct (BearerTokenKey above is one) is not such a key: the runtime may
// place two zero-size variables at the same address, and the user's slot then
// reads whatever the other key stored — the caller's bearer token.
type userKey struct{}

// WithUser returns a copy of ctx that carries the authenticated user of the
// turn every model call made under ctx belongs to; the call sends it as the
// x-kagent-user header. The A2A server sets it from the metadata of the same
// name the gateway forwards once it has resolved the caller's identity from a
// validated token; a turn without one carries nothing and its calls send no
// such header.
func WithUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

// UserFromContext returns the user set by WithUser, or "" when there is none.
func UserFromContext(ctx context.Context) string {
	user, _ := ctx.Value(userKey{}).(string)
	return user
}

// runtimeIdentityHeaders is the identity the controller injected into this
// runtime — the name of the AgentTemplate it executes (KAGENT_AGENT_TEMPLATE)
// and its namespace (KAGENT_NAMESPACE) — as the headers every model call
// carries. A runtime the controller did not start (a BYO build, a test) has no
// identity and sends none; half an identity is none.
func runtimeIdentityHeaders() map[string]string {
	agent, hasAgent := env.KagentAgentTemplate.Lookup()
	namespace, hasNamespace := env.KagentNamespace.Lookup()
	if !hasAgent || !hasNamespace || agent == "" || namespace == "" {
		return nil
	}
	return map[string]string{adk.AgentHeader: agent, adk.AgentNamespaceHeader: namespace}
}

// headerTransport wraps an http.RoundTripper and sets headers on every request:
// the configured default headers, then the runtime's identity — which therefore
// wins over a configured header of the same name, so a model configuration
// cannot name another agent — then the user of the turn the request belongs to.
type headerTransport struct {
	base     http.RoundTripper
	headers  map[string]string
	identity map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	for k, v := range t.identity {
		req.Header.Set(k, v)
	}
	if user := UserFromContext(req.Context()); user != "" {
		req.Header.Set(adk.UserHeader, user)
	}
	return t.base.RoundTrip(req)
}

// nonNilFunctionCallArgs returns args, substituting an empty map for nil.
// A no-argument FunctionCall reloads from the session store with nil Args
// (omitempty drops the empty map), and some providers reject null or "null".
//
// TODO: remove once the pinned go-genai fixes
// https://github.com/googleapis/go-genai/issues/920.
func nonNilFunctionCallArgs(args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	return args
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

// unsupportedImageNote stands in for an image a text-only adapter cannot send,
// so an image-only turn is not dropped.
func unsupportedImageNote(blob *genai.Blob) string {
	name := blob.DisplayName
	if name == "" {
		name = "image"
	}
	return fmt.Sprintf("[Image %q was not sent: this provider integration cannot pass images.]", name)
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

// mergeSystemInstructionFromConfig appends Config.SystemInstruction parts to any
// system text extracted from conversation contents. The Google ADK delivers the
// agent Instruction via Config.SystemInstruction, not as role-"system" Content.
func mergeSystemInstructionFromConfig(existing string, config *genai.GenerateContentConfig) string {
	if config == nil || config.SystemInstruction == nil {
		return strings.TrimSpace(existing)
	}
	var b strings.Builder
	b.WriteString(existing)
	for _, p := range config.SystemInstruction.Parts {
		if p != nil && p.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
	}
	return strings.TrimSpace(b.String())
}
