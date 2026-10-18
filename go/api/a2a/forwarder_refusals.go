package a2a

import (
	"math"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// ForwarderRefusalsMetadataKey carries, on a turn's status message, what the
// runtime's loopback MCP forwarder refused since the previous turn boundary.
const ForwarderRefusalsMetadataKey = MetadataPrefix + "forwarder-refusals"

// ForwarderRefusals counts the requests a loopback MCP forwarder answered
// itself instead of forwarding, by the status it answered with. The agent's
// model client holds the forwarder's token and reaches only the servers it was
// given, so each count is a request from another process in the sandbox, or
// from a client that read the forwarder's configuration and probes past it.
type ForwarderRefusals struct {
	// Unauthorized counts requests without the forwarder's token (401).
	Unauthorized int64
	// UnknownServer counts requests for a server the forwarder does not front (404).
	UnknownServer int64
	// RefusedPath counts requests whose path could climb out of its server (400).
	RefusedPath int64
}

// IsZero reports whether nothing was refused.
func (r ForwarderRefusals) IsZero() bool { return r == ForwarderRefusals{} }

var forwarderRefusalFields = []struct {
	name  string
	count func(*ForwarderRefusals) *int64
}{
	{"unauthorized", func(r *ForwarderRefusals) *int64 { return &r.Unauthorized }},
	{"unknownServer", func(r *ForwarderRefusals) *int64 { return &r.UnknownServer }},
	{"refusedPath", func(r *ForwarderRefusals) *int64 { return &r.RefusedPath }},
}

// MetadataValue is the value refusals take under ForwarderRefusalsMetadataKey.
func (r ForwarderRefusals) MetadataValue() map[string]any {
	fields := make(map[string]any, len(forwarderRefusalFields))
	for _, field := range forwarderRefusalFields {
		fields[field.name] = *field.count(&r)
	}
	return fields
}

// SetForwarderRefusals records refusals on a status message; nothing refused
// records nothing.
func SetForwarderRefusals(carrier a2atype.MetadataCarrier, refusals ForwarderRefusals) {
	if carrier == nil || refusals.IsZero() {
		return
	}
	carrier.SetMeta(ForwarderRefusalsMetadataKey, refusals.MetadataValue())
}

// ForwarderRefusalsOf reads the refusals a status message records. A message
// without them reads as zero; malformed ones, a count that is not a
// non-negative integer, read as false.
func ForwarderRefusalsOf(carrier a2atype.MetadataCarrier) (ForwarderRefusals, bool) {
	var refusals ForwarderRefusals
	if carrier == nil {
		return refusals, true
	}
	value, present := carrier.Meta()[ForwarderRefusalsMetadataKey]
	if !present {
		return refusals, true
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return ForwarderRefusals{}, false
	}
	for _, field := range forwarderRefusalFields {
		raw, present := fields[field.name]
		if !present {
			continue
		}
		count, ok := nonNegativeCount(raw)
		if !ok {
			return ForwarderRefusals{}, false
		}
		*field.count(&refusals) = count
	}
	return refusals, true
}

// nonNegativeCount accepts the integer kinds a status message holds in
// memory and the float64 it holds once it went through JSON or protobuf.
func nonNegativeCount(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, number >= 0
	case int:
		return int64(number), number >= 0
	case float64:
		if number < 0 || number != math.Trunc(number) || number > math.MaxInt64/2 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}
