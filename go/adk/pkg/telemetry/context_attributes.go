package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"go.opentelemetry.io/contrib/processors/baggagecopy"
	"go.opentelemetry.io/otel/baggage"
)

const (
	maxContextKeys      = 32
	maxContextKeyLength = 64
)

// Names come from env.KagentTraceContextKeys so the controller and the
// runtime share one source of truth.
var traceContextKeysEnvVar = env.KagentTraceContextKeys.Name()

// contextMapping is one allowlisted promotion: read source from baggage or
// A2A metadata and write it to baggage as attribute. The upstream
// baggagecopy processor then copies that baggage key onto every span.
type contextMapping struct {
	source    string
	attribute string
}

// contextPolicy is the parsed allowlist plus every source key named in it.
// Source keys are skipped by SetMessageMetadataAttributes so allowlisted
// values are not also stamped as a2a.message.metadata.<from>.
type contextPolicy struct {
	mappings []contextMapping
	sources  map[string]struct{}
}

// loadContextPolicy parses KAGENT_TRACE_CONTEXT_KEYS once. The allowlist
// cannot change without a process restart; tests call
// resetAllowedContextMappings after t.Setenv.
var loadContextPolicy = sync.OnceValue(parseContextPolicy)

func resetAllowedContextMappings() {
	loadContextPolicy = sync.OnceValue(parseContextPolicy)
}

func allowedContextMappings() []contextMapping {
	return loadContextPolicy().mappings
}

func policyCoveredContextSources() map[string]struct{} {
	return loadContextPolicy().sources
}

func parseContextPolicy() contextPolicy {
	mappings := parseAllowedContextMappings()
	sources := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		sources[mapping.source] = struct{}{}
	}
	return contextPolicy{mappings: mappings, sources: sources}
}

// ContextWithPromotedMetadata copies allowlisted A2A message.metadata values
// into W3C baggage when the destination key is not already set. Existing
// baggage members named in from are remapped onto to the same way, so both
// sources share one path: the baggagecopy span processor.
//
// Empty or non-scalar metadata does not wipe baggage. Invalid baggage
// members are skipped rather than failing the request.
func ContextWithPromotedMetadata(ctx context.Context, metadata map[string]any) context.Context {
	mappings := allowedContextMappings()
	if len(mappings) == 0 {
		return ctx
	}

	bag := baggage.FromContext(ctx)
	changed := false
	for _, mapping := range mappings {
		if bag.Member(mapping.attribute).Value() != "" {
			continue
		}
		value := ""
		if scalar, ok := scalarString(metadata[mapping.source]); ok {
			value = sanitizeContextValue(scalar)
		}
		if value == "" && mapping.source != mapping.attribute {
			value = sanitizeContextValue(bag.Member(mapping.source).Value())
		}
		if value == "" {
			continue
		}
		member, err := baggage.NewMember(mapping.attribute, value)
		if err != nil {
			continue
		}
		next, err := bag.SetMember(member)
		if err != nil {
			continue
		}
		bag = next
		changed = true
	}
	if !changed {
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, bag)
}

// AllowedBaggageCopyFilter returns a baggagecopy predicate for the
// allowlisted destination keys, or nil when promotion is off.
func AllowedBaggageCopyFilter() baggagecopy.Filter {
	mappings := allowedContextMappings()
	if len(mappings) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		allowed[mapping.attribute] = struct{}{}
	}
	return func(member baggage.Member) bool {
		_, ok := allowed[member.Key()]
		return ok
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
		if mapping, ok := newContextMapping(strings.TrimSpace(key), ""); ok {
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
			if mapping, ok := newContextMapping(key, ""); ok {
				mappings = append(mappings, mapping)
			}
			continue
		}
		var spec struct {
			From string          `json:"from"`
			To   string          `json:"to"`
			Hash json.RawMessage `json:"hash"`
		}
		if err := json.Unmarshal(item, &spec); err != nil {
			continue
		}
		// Leftover {hash:...} config is no longer supported. Drop the
		// mapping rather than stamping the raw source value onto `to`.
		if spec.Hash != nil {
			continue
		}
		if mapping, ok := newContextMapping(spec.From, spec.To); ok {
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}

func newContextMapping(from, to string) (contextMapping, bool) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" || len([]rune(from)) > maxContextKeyLength || !isAttributeKey(from) {
		return contextMapping{}, false
	}
	if to == "" {
		to = from
	}
	if len([]rune(to)) > maxContextKeyLength || !isAttributeKey(to) {
		return contextMapping{}, false
	}
	return contextMapping{source: from, attribute: to}, true
}

func capMappings(mappings []contextMapping) []contextMapping {
	out := make([]contextMapping, 0, maxContextKeys)
	seen := make(map[string]struct{}, maxContextKeys)
	for _, mapping := range mappings {
		id := mapping.source + "\x00" + mapping.attribute
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

func isAttributeKey(key string) bool {
	return strings.IndexFunc(key, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) < 0
}

// scalarString renders a JSON scalar from A2A message metadata as a string.
// Objects and arrays are skipped: they are unbounded in size and carry no
// useful meaning as a baggage value. JSON and protobuf Struct decode numbers
// as float64, so that is the only numeric case.
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
