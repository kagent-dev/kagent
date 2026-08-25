package telemetry

import (
	"context"
	"os"
	"strconv"
	"strings"
	"unicode"

	"go.opentelemetry.io/otel/baggage"
)

const (
	// traceContextKeysEnvVar holds a comma-separated allowlist of caller-supplied
	// context keys to promote onto agent spans. Unset or empty (the default)
	// disables promotion entirely.
	traceContextKeysEnvVar = "KAGENT_TRACE_CONTEXT_KEYS"

	// contextAttributePrefix namespaces every promoted value. Because the prefix
	// is applied unconditionally, caller-supplied data cannot shadow a semantic
	// convention attribute such as service.name.
	contextAttributePrefix = "kagent.context."

	maxContextKeys        = 32
	maxContextKeyLength   = 64
	maxContextValueLength = 256
)

// CallerContextAttributes returns the caller-supplied context values that an
// operator allowlisted through KAGENT_TRACE_CONTEXT_KEYS. Merge the result into
// the request-scoped attribute bag (see SetKAgentSpanAttributes) so every span
// of the request carries them: trace-level filtering in backends such as
// Langfuse matches on each span, not only on the root.
//
// Values are read from W3C baggage first and then from the A2A message
// metadata, which is the more specific source for a single message and
// therefore wins. Both are untrusted input, so keys must appear in the
// allowlist, values are stripped of control characters and truncated, and every
// attribute is namespaced under contextAttributePrefix.
//
// Returns nil when the allowlist is empty, which is the default.
func CallerContextAttributes(ctx context.Context, metadata map[string]any) map[string]string {
	keys := allowedContextKeys()
	if len(keys) == 0 {
		return nil
	}

	bag := baggage.FromContext(ctx)
	attrs := make(map[string]string, len(keys))
	for _, key := range keys {
		value := sanitizeContextValue(bag.Member(key).Value())
		if scalar, ok := scalarString(metadata[key]); ok {
			value = sanitizeContextValue(scalar)
		}
		if value == "" {
			continue
		}
		attrs[contextAttributePrefix+key] = value
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// allowedContextKeys parses the KAGENT_TRACE_CONTEXT_KEYS allowlist. Keys that
// are empty, over-long, or contain whitespace or control characters are
// dropped, and the list is capped at maxContextKeys so a misconfigured
// allowlist cannot inflate span cardinality without bound.
func allowedContextKeys() []string {
	raw := strings.TrimSpace(os.Getenv(traceContextKeysEnvVar))
	if raw == "" {
		return nil
	}

	keys := make([]string, 0, maxContextKeys)
	seen := make(map[string]struct{}, maxContextKeys)
	for key := range strings.SplitSeq(raw, ",") {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > maxContextKeyLength || !isAttributeKey(key) {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
		if len(keys) == maxContextKeys {
			break
		}
	}
	return keys
}

// isAttributeKey reports whether key is safe to use as a span attribute name.
func isAttributeKey(key string) bool {
	return strings.IndexFunc(key, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) < 0
}

// scalarString renders a JSON scalar from A2A message metadata as a string.
// Objects and arrays are skipped: they are unbounded in size and carry no
// useful meaning as a span attribute value.
func scalarString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	default:
		return "", false
	}
}

// sanitizeContextValue makes an untrusted value safe to attach to a span:
// control characters are dropped so a value cannot forge structure in a
// downstream trace or log renderer, and the result is truncated to bound the
// size of exported spans.
func sanitizeContextValue(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	cleaned = strings.TrimSpace(cleaned)
	// Truncate by rune, not byte, so the limit means the same thing here as it
	// does in the Python runtime.
	if runes := []rune(cleaned); len(runes) > maxContextValueLength {
		cleaned = string(runes[:maxContextValueLength])
	}
	return cleaned
}
