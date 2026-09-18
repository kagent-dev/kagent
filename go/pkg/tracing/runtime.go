package tracing

import (
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

// Span and resource attribute keys produced by Harness runtimes. Consumers
// read these names directly, so treat them as a published contract.
const (
	// AttributeHarnessKind names the native runtime behind an invocation. A
	// Harness object's configurable name is not its kind.
	AttributeHarnessKind = "kagent.harness.kind"
	// AttributeAgentName is the compiled agent identity, <template>-<harness>.
	AttributeAgentName = "gen_ai.agent.name"
	// AttributeConversationID is the A2A context that groups a conversation.
	AttributeConversationID = "gen_ai.conversation.id"
	// AttributeTaskID is the A2A task an invocation executes.
	AttributeTaskID = "gen_ai.task.id"
	// AttributeUserID is the gateway-established user, absent when no trusted
	// identity reached the runtime.
	AttributeUserID = "kagent.user_id"
	// AttributeTaskState is the A2A state execution actually reported.
	AttributeTaskState = "a2a.task.state"
	// AttributeSegment distinguishes the first execution of a task from the
	// segments that continue it after an approval or question.
	AttributeSegment = "kagent.invocation.segment"
	// AttributeDisposition records how a segment stopped when that is not
	// visible from the task state, such as an abandoned client stream.
	AttributeDisposition = "kagent.invocation.disposition"
	// AttributeInput and AttributeOutput carry bounded turn content, recorded
	// only under the content-capture opt-in.
	AttributeInput           = "kagent.input"
	AttributeInputTruncated  = "kagent.input.truncated"
	AttributeOutput          = "kagent.output"
	AttributeOutputTruncated = "kagent.output.truncated"
	// AttributeErrorType is a safe failure category. It never carries provider
	// responses, credentials, or captured content.
	AttributeErrorType = "error.type"
	// AttributeLinkRelationship describes why a segment links to another span.
	AttributeLinkRelationship = "kagent.invocation.relationship"
)

// Segment values for AttributeSegment.
const (
	SegmentInitial = "initial"
	SegmentResumed = "resumed"
)

// Disposition values for AttributeDisposition.
const (
	// DispositionAbandoned means the A2A event consumer stopped reading. It is
	// deliberately distinct from cancellation, which the client must request.
	DispositionAbandoned = "abandoned"
	// DispositionCanceled means cancellation was requested for this task.
	DispositionCanceled = "canceled"
	// DispositionInterrupted means the execution context ended without a
	// cancellation request, such as a runtime shutting down mid-turn.
	DispositionInterrupted = "interrupted"
)

// RelationshipResumeOrigin marks the link from a resumed segment back to the
// segment that parked the task. A link states a relationship; it does not
// reparent spans or transfer ownership of token usage.
const RelationshipResumeOrigin = "resume_origin"

// HarnessKind identifies the native runtime a Harness compiles to.
type HarnessKind string

const (
	HarnessKindClaude HarnessKind = "claude"
	HarnessKindCodex  HarnessKind = "codex"
)

// DefaultCaptureBytes bounds captured input and output when a configuration
// enables capture without choosing a limit.
const DefaultCaptureBytes = 16 << 10

// MaxCaptureBytes is the hard ceiling on captured input and output. It bounds
// per-request memory and the payload each invocation adds to the exporter
// queue, independently of response length.
const MaxCaptureBytes = 64 << 10

// RuntimeTelemetry is the compiler-owned telemetry contract carried in a
// Harness runtime configuration. It supplies the static identity every span
// and resource needs and the content-capture policy the runtime enforces.
// Request identity is never part of it, since one runtime process serves many
// conversations, tasks, and users.
//
// A configuration without this section stays valid: the runtime keeps its
// environment-derived service identity and capture stays off.
type RuntimeTelemetry struct {
	HarnessKind     HarnessKind `json:"harness_kind,omitempty"`
	AgentName       string      `json:"agent_name,omitempty"`
	AgentNamespace  string      `json:"agent_namespace,omitempty"`
	CaptureContent  bool        `json:"capture_content,omitempty"`
	MaxCaptureBytes int         `json:"max_capture_bytes,omitempty"`
}

// Validate rejects configurations a compiler could not have produced.
func (t RuntimeTelemetry) Validate() error {
	switch t.HarnessKind {
	case "", HarnessKindClaude, HarnessKindCodex:
	default:
		return fmt.Errorf("unsupported harness kind %q", t.HarnessKind)
	}
	// Identity is all or nothing. A partial identity would mark a span as a
	// harness invocation while leaving a user-supplied agent name
	// authoritative, since Identity omits the keys it does not have.
	present := 0
	for _, value := range []string{string(t.HarnessKind), t.AgentName, t.AgentNamespace} {
		if value != "" {
			present++
		}
	}
	if present != 0 && present != 3 {
		return fmt.Errorf("telemetry identity requires a harness kind, agent name, and namespace together")
	}
	if strings.TrimSpace(t.AgentName) != t.AgentName || strings.TrimSpace(t.AgentNamespace) != t.AgentNamespace {
		return fmt.Errorf("telemetry agent identity must not have surrounding whitespace")
	}
	if t.MaxCaptureBytes < 0 || t.MaxCaptureBytes > MaxCaptureBytes {
		return fmt.Errorf("telemetry capture limit must be between 0 and %d bytes", MaxCaptureBytes)
	}
	return nil
}

// Identity returns the trusted static attributes stamped on every invocation
// span and on the runtime resource.
func (t RuntimeTelemetry) Identity() []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, 2)
	if t.HarnessKind != "" {
		attributes = append(attributes, attribute.String(AttributeHarnessKind, string(t.HarnessKind)))
	}
	if t.AgentName != "" {
		attributes = append(attributes, attribute.String(AttributeAgentName, t.AgentName))
	}
	return attributes
}

// CaptureLimit is the effective byte budget for one captured input or output.
// It is zero when capture is disabled.
func (t RuntimeTelemetry) CaptureLimit() int {
	if !t.CaptureContent {
		return 0
	}
	if t.MaxCaptureBytes <= 0 {
		return DefaultCaptureBytes
	}
	if t.MaxCaptureBytes > MaxCaptureBytes {
		return MaxCaptureBytes
	}
	return t.MaxCaptureBytes
}

// MergeResourceAttributes renders an OTEL_RESOURCE_ATTRIBUTES value that keeps
// user-supplied attributes and makes owned keys authoritative. Native child
// processes inherit their identity this way, so a user tuning unrelated
// attributes cannot displace it and a user-supplied marker is never required.
//
// Entries without a key are dropped. A repeated key keeps its last value, which
// is what the OpenTelemetry SDK resolves the same string to, so a runtime and
// the native process it supervises cannot disagree about a user-supplied attribute.
func MergeResourceAttributes(existing string, owned []attribute.KeyValue) string {
	ownedKeys := make(map[string]struct{}, len(owned))
	for _, attr := range owned {
		ownedKeys[string(attr.Key)] = struct{}{}
	}
	merged := make([]string, 0, len(owned)+4)
	seen := make(map[string]int, len(owned))
	for entry := range strings.SplitSeq(existing, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, _, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		if _, owned := ownedKeys[key]; owned {
			continue
		}
		if position, duplicate := seen[key]; duplicate {
			merged[position] = entry
			continue
		}
		seen[key] = len(merged)
		merged = append(merged, entry)
	}
	ownedEntries := make([]string, 0, len(owned))
	for _, attr := range owned {
		value := strings.TrimSpace(attr.Value.String())
		if value == "" {
			continue
		}
		ownedEntries = append(ownedEntries, string(attr.Key)+"="+encodeResourceValue(value))
	}
	slices.Sort(ownedEntries)
	return strings.Join(append(merged, ownedEntries...), ",")
}

// encodeResourceValue applies the W3C Baggage percent-encoding that the OTEL
// specification requires of OTEL_RESOURCE_ATTRIBUTES values.
func encodeResourceValue(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 0x21 && character <= 0x7e && character != '"' && character != ',' &&
			character != ';' && character != '\\' && character != '=' && character != '%' {
			builder.WriteByte(character)
			continue
		}
		fmt.Fprintf(&builder, "%%%02X", character)
	}
	return builder.String()
}

// ResourceEnvironmentVariable is the standard OTEL resource attribute carrier.
const ResourceEnvironmentVariable = "OTEL_RESOURCE_ATTRIBUTES"

// ResourceEnvironment merges owned attributes into the OTEL_RESOURCE_ATTRIBUTES
// entry of a child process environment, so a native runtime reports the same
// identity as the Go wrapper that supervises it. User-supplied attributes
// survive; owned keys are replaced. The variable is dropped when the merge
// leaves nothing to set.
func ResourceEnvironment(environment []string, owned []attribute.KeyValue) []string {
	prefix := ResourceEnvironmentVariable + "="
	existing := ""
	result := make([]string, 0, len(environment)+1)
	for _, variable := range environment {
		if value, found := strings.CutPrefix(variable, prefix); found {
			existing = value
			continue
		}
		result = append(result, variable)
	}
	merged := MergeResourceAttributes(existing, owned)
	if merged == "" {
		return result
	}
	return append(result, prefix+merged)
}
