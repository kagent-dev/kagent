// Package headers resolves per-request HTTP headers from the incoming A2A
// call context. It is shared by the MCP tool transport (allowedHeaders) and
// the model transport (passthroughHeaders) so both forward caller-supplied
// headers with identical semantics.
package headers

import (
	"context"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// restrictedPassthroughHeaders are names that must never be forwarded from a
// caller onto another hop:
//   - credential headers (Authorization, Proxy-Authorization, Cookie) would
//     forward a caller credential onward or clobber the credential the
//     receiving hop manages itself; apiKeyPassthrough is the supported
//     mechanism for credential forwarding;
//   - the rest are hop-by-hop or message-framing headers per RFC 9110, plus
//     the non-standard Proxy-Connection.
//
// Must stay in sync with RESTRICTED_PASSTHROUGH_HEADERS in
// python/packages/kagent-adk/src/kagent/adk/_llm_header_passthrough_plugin.py.
var restrictedPassthroughHeaders = map[string]struct{}{
	"authorization":       {},
	"connection":          {},
	"content-length":      {},
	"cookie":              {},
	"host":                {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"proxy-connection":    {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

// IsRestricted reports whether a header name (case-insensitively) must never
// be forwarded from a caller onto another hop.
func IsRestricted(name string) bool {
	_, restricted := restrictedPassthroughHeaders[strings.ToLower(name)]
	return restricted
}

// FilterRestricted drops restricted names (case-insensitively) from a
// configured pass-through header list.
func FilterRestricted(names []string) []string {
	var out []string
	for _, n := range names {
		if !IsRestricted(n) {
			out = append(out, n)
		}
	}
	return out
}

// AllowedRequestHeaders reads the incoming A2A request metadata from ctx and
// returns only the header key/value pairs whose names appear in allowed.
// It reads directly from the A2A CallContext that is already present in the Go
// context, avoiding a redundant copy.
//
// Lookup relies on ServiceParams.Get, which does a case-insensitive lookup
// (NewServiceParams lowercases keys at construction). Keys in the result
// preserve the casing from the allowed list so the receiving server sees the
// header names the operator configured. When a header has multiple values only
// the first one is forwarded; additional values are intentionally dropped.
func AllowedRequestHeaders(ctx context.Context, allowed []string) map[string]string {
	if len(allowed) == 0 {
		return nil
	}
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return nil
	}
	meta := callCtx.ServiceParams()
	if meta == nil {
		return nil
	}
	result := make(map[string]string)
	for _, name := range allowed {
		if vals, ok := meta.Get(name); ok && len(vals) > 0 && vals[0] != "" {
			result[name] = vals[0]
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
