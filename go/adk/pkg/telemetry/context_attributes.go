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
	"unicode"

	"go.opentelemetry.io/otel/baggage"
)

const (
	// traceContextKeysEnvVar holds the allowlist of caller-supplied context
	// keys to promote onto agent spans. Unset or empty (the default) disables
	// promotion entirely.
	//
	// Accepts a comma-separated list of source keys, or a JSON array of strings
	// and {from, to, hash} objects. See allowedContextMappings.
	traceContextKeysEnvVar = "KAGENT_TRACE_CONTEXT_KEYS"

	// traceContextHashKeyEnvVar is the HMAC key used when a mapping sets
	// hash: hmac-sha256. Required for those entries; without it the hashed
	// attribute is skipped rather than emitted in plaintext.
	traceContextHashKeyEnvVar = "KAGENT_TRACE_CONTEXT_HASH_KEY"

	// contextAttributePrefix namespaces custom promoted values so they cannot
	// shadow a semantic convention attribute such as service.name. Registry
	// names (user.*, enduser.*, session.id) and names already in the kagent.
	// namespace are left unprefixed; see spanAttributeName.
	contextAttributePrefix = "kagent.context."

	hashHMACSHA256 = "hmac-sha256"

	maxContextKeys        = 32
	maxContextKeyLength   = 64
	maxContextValueLength = 256
)

// contextMapping is one allowlisted promotion: read source from baggage or
// A2A metadata and emit it as span attribute attribute (after prefix rules).
type contextMapping struct {
	source    string
	attribute string
	hash      string
}

// CallerContextAttributes returns the caller-supplied context values that an
// operator allowlisted through KAGENT_TRACE_CONTEXT_KEYS. Merge the result into
// the request-scoped attribute bag (see SetKAgentSpanAttributes) so every span
// of the request carries them: trace-level filtering in backends such as
// Langfuse matches on each span, not only on the root.
//
// Values are read from W3C baggage first and then from the A2A message
// metadata, which is the more specific source for a single message and
// therefore wins. Both are untrusted input, so keys must appear in the
// allowlist, values are stripped of control characters and truncated, and
// attribute names go through spanAttributeName.
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
			value = sanitizeContextValue(scalar)
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

// allowedContextMappings parses the KAGENT_TRACE_CONTEXT_KEYS allowlist.
// Entries that are empty, over-long, or contain whitespace or control
// characters are dropped, and the list is capped at maxContextKeys so a
// misconfigured allowlist cannot inflate span cardinality without bound.
func allowedContextMappings() []contextMapping {
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
	if from == "" || len(from) > maxContextKeyLength || !isAttributeKey(from) {
		return contextMapping{}, false
	}
	if to == "" {
		to = from
	}
	if len(to) > maxContextKeyLength || !isAttributeKey(to) {
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
// user.*, enduser.*, and session.id pass through unprefixed so operators can
// use the semantic convention names. Names already in the kagent. namespace
// are left as-is. Everything else is placed under kagent.context. so a
// caller-supplied service.name cannot shadow the real one.
func spanAttributeName(name string) string {
	if isRegistryAttribute(name) || strings.HasPrefix(name, "kagent.") {
		return name
	}
	return contextAttributePrefix + name
}

func isRegistryAttribute(name string) bool {
	return strings.HasPrefix(name, "user.") ||
		strings.HasPrefix(name, "enduser.") ||
		name == "session.id"
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
