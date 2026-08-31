package telemetry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"go.opentelemetry.io/otel/baggage"
)

const (
	// contextAttributePrefix namespaces custom promoted values so they cannot
	// shadow a semantic convention attribute such as service.name. Only the
	// explicit registry names in unprefixedAttributes pass through as-is;
	// see spanAttributeName.
	contextAttributePrefix = "kagent.context."

	hashHMACSHA256 = "hmac-sha256"

	maxContextKeys        = 32
	maxContextKeyLength   = 64
	maxContextValueLength = 256
)

// Names come from env.KagentTraceContextKeys / HashKey so the controller and
// the runtime share one source of truth.
var (
	traceContextKeysEnvVar    = env.KagentTraceContextKeys.Name()
	traceContextHashKeyEnvVar = env.KagentTraceContextHashKey.Name()
)

// unprefixedAttributes is the OpenTelemetry registry names callers may emit
// without a kagent.context. prefix. It is an explicit set so an invented
// name such as user.asdasd cannot masquerade as a semantic convention.
var unprefixedAttributes = map[string]struct{}{
	"user.id":    {},
	"user.hash":  {},
	"enduser.id": {},
	"session.id": {},
}

// contextMapping is one allowlisted promotion: read source from baggage or
// A2A metadata and emit it as span attribute attribute (after prefix rules).
type contextMapping struct {
	source    string
	attribute string
	hash      string
}

// loadAllowedContextMappings parses KAGENT_TRACE_CONTEXT_KEYS once. The
// allowlist cannot change without a process restart; tests call
// resetAllowedContextMappings after t.Setenv.
var loadAllowedContextMappings = sync.OnceValue(parseAllowedContextMappings)

func resetAllowedContextMappings() {
	loadAllowedContextMappings = sync.OnceValue(parseAllowedContextMappings)
}

func allowedContextMappings() []contextMapping {
	return loadAllowedContextMappings()
}

// CallerContextAttributes returns the caller-supplied context values that an
// operator allowlisted through KAGENT_TRACE_CONTEXT_KEYS. Merge the result into
// the request-scoped attribute bag (see SetKAgentSpanAttributes) so every span
// of the request carries them: trace-level filtering in backends such as
// Langfuse matches on each span, not only on the root.
//
// Values are read from W3C baggage first and then from the A2A message
// metadata, which is the more specific source for a single message and
// therefore wins — but only when the sanitised metadata scalar is non-empty,
// so a blank metadata entry cannot wipe a baggage value. Both are untrusted
// input, so keys must appear in the allowlist, values are stripped of control
// characters and truncated, and attribute names go through spanAttributeName.
//
// Returns nil when the allowlist is empty, which is the default.
func CallerContextAttributes(ctx context.Context, metadata map[string]any) map[string]string {
	mappings := allowedContextMappings()
	if len(mappings) == 0 {
		return nil
	}

	bag := baggage.FromContext(ctx)
	attrs := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		value := sanitizeContextValue(bag.Member(mapping.source).Value())
		if scalar, ok := scalarString(metadata[mapping.source]); ok {
			if cleaned := sanitizeContextValue(scalar); cleaned != "" {
				value = cleaned
			}
		}
		if value == "" {
			continue
		}
		if mapping.hash != "" {
			value = hashContextValue(value, mapping.hash)
			if value == "" {
				continue
			}
		} else {
			value = truncateContextValue(value)
		}
		name := spanAttributeName(mapping.attribute)
		if _, exists := attrs[name]; exists {
			continue
		}
		attrs[name] = value
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// MergeCallerContextAttributes copies promoted caller context into dst without
// replacing keys that are already set, so caller-supplied data cannot override
// kagent.user_id, kagent.app_name, or other attributes the runtime already
// stamped.
func MergeCallerContextAttributes(dst map[string]string, ctx context.Context, metadata map[string]any) {
	for key, value := range CallerContextAttributes(ctx, metadata) {
		if _, exists := dst[key]; !exists {
			dst[key] = value
		}
	}
}

func parseAllowedContextMappings() []contextMapping {
	raw := strings.TrimSpace(os.Getenv(traceContextKeysEnvVar))
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		return capMappings(parseJSONAllowlist(raw))
	}
	return capMappings(parseCommaAllowlist(raw))
}

func parseCommaAllowlist(raw string) []contextMapping {
	mappings := make([]contextMapping, 0, maxContextKeys)
	for key := range strings.SplitSeq(raw, ",") {
		if mapping, ok := newContextMapping(strings.TrimSpace(key), "", ""); ok {
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}

func parseJSONAllowlist(raw string) []contextMapping {
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	mappings := make([]contextMapping, 0, maxContextKeys)
	for _, item := range items {
		var key string
		if err := json.Unmarshal(item, &key); err == nil {
			if mapping, ok := newContextMapping(key, "", ""); ok {
				mappings = append(mappings, mapping)
			}
			continue
		}
		var spec struct {
			From string `json:"from"`
			To   string `json:"to"`
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(item, &spec); err != nil {
			continue
		}
		if mapping, ok := newContextMapping(spec.From, spec.To, spec.Hash); ok {
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}

func newContextMapping(from, to, hash string) (contextMapping, bool) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	hash = strings.TrimSpace(hash)
	if from == "" || len([]rune(from)) > maxContextKeyLength || !isAttributeKey(from) {
		return contextMapping{}, false
	}
	if to == "" {
		to = from
	}
	if len([]rune(to)) > maxContextKeyLength || !isAttributeKey(to) {
		return contextMapping{}, false
	}
	if hash != "" && hash != hashHMACSHA256 {
		return contextMapping{}, false
	}
	return contextMapping{source: from, attribute: to, hash: hash}, true
}

func capMappings(mappings []contextMapping) []contextMapping {
	out := make([]contextMapping, 0, maxContextKeys)
	seen := make(map[string]struct{}, maxContextKeys)
	for _, mapping := range mappings {
		id := mapping.source + "\x00" + mapping.attribute + "\x00" + mapping.hash
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, mapping)
		if len(out) == maxContextKeys {
			break
		}
	}
	return out
}

// spanAttributeName is the name written onto the span.
//
// user.id, user.hash, enduser.id, and session.id pass through unprefixed so
// operators can use the semantic convention names. Everything else is placed
// under kagent.context. so a caller-supplied service.name or kagent.user_id
// cannot shadow the real one.
func spanAttributeName(name string) string {
	if _, ok := unprefixedAttributes[name]; ok {
		return name
	}
	return contextAttributePrefix + name
}

// isAttributeKey reports whether key is safe to use as a span attribute name.
func isAttributeKey(key string) bool {
	return strings.IndexFunc(key, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) < 0
}

// scalarString renders a JSON scalar from A2A message metadata as a string.
// Objects and arrays are skipped: they are unbounded in size and carry no
// useful meaning as a span attribute value. JSON and protobuf Struct decode
// numbers as float64, so that is the only numeric case.
func scalarString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	default:
		return "", false
	}
}

// sanitizeContextValue drops control characters and trims space so a value
// cannot forge structure in a downstream trace or log renderer.
func sanitizeContextValue(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	return strings.TrimSpace(cleaned)
}

func truncateContextValue(value string) string {
	// Truncate by rune, not byte, so the limit means the same thing here as it
	// does in the Python runtime.
	if runes := []rune(value); len(runes) > maxContextValueLength {
		return string(runes[:maxContextValueLength])
	}
	return value
}

// hashContextValue hashes value with the requested algorithm. Unknown
// algorithms and a missing HMAC key skip the attribute: never fall back to
// putting the original value on the span.
func hashContextValue(value, algorithm string) string {
	if algorithm != hashHMACSHA256 {
		return ""
	}
	key := os.Getenv(traceContextHashKeyEnvVar)
	if key == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
