package tracing

import (
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// SchemaURL is the OpenTelemetry semantic conventions version this contract
// follows. Every tracer kagent creates declares it, so a consumer can tell
// which revision of the GenAI conventions a span's attribute names come from.
const SchemaURL = semconv.SchemaURL

// Span and resource attribute keys kagent runtimes produce. Consumers read
// these names directly, so treat them as a published contract. Names the GenAI
// semantic conventions define are taken from the pinned semconv package rather
// than spelled out, so they cannot drift from the version SchemaURL declares;
// only the kagent and A2A namespaces are kagent's own.
const (
	// AttributeOperationName is the GenAI operation. An invocation span carries
	// OperationInvokeAgent; model and tool spans come from the runtime itself.
	AttributeOperationName = string(semconv.GenAIOperationNameKey)
	// AttributeRuntime names the runtime behind an invocation. A Harness
	// object's configurable name is not its runtime.
	AttributeRuntime = "kagent.runtime"
	// AttributeAgentName is the compiled agent identity, <template>-<harness>.
	AttributeAgentName = string(semconv.GenAIAgentNameKey)
	// AttributeAgentID is the agent identity qualified by its namespace, which
	// is what makes it unique within a cluster.
	AttributeAgentID = string(semconv.GenAIAgentIDKey)
	// AttributeProviderName is the model provider the agent is compiled against,
	// in the GenAI conventions' vocabulary.
	AttributeProviderName = string(semconv.GenAIProviderNameKey)
	// AttributeRequestModel is the model the agent is compiled against. The
	// conventions allow it on an agent span only when the agent is bound to one
	// model, which a compiled kagent agent is.
	AttributeRequestModel = string(semconv.GenAIRequestModelKey)
	// AttributeConversationID is the A2A context that groups a conversation.
	AttributeConversationID = string(semconv.GenAIConversationIDKey)
	// AttributeTaskID is the A2A task an invocation executes. The conventions
	// have no task identity, so it stays in the A2A namespace.
	AttributeTaskID = "a2a.task.id"
	// AttributeUserID is the gateway-established user, absent when no trusted
	// identity reached the runtime.
	AttributeUserID = string(semconv.EnduserIDKey)
	// AttributeMethod is the A2A method that started the invocation.
	AttributeMethod = "a2a.method"
	// AttributeTaskState is the A2A state execution actually reported.
	AttributeTaskState = "a2a.task.state"
	// AttributeSegment distinguishes the first execution of a task from the
	// segments that continue it after an approval or question.
	AttributeSegment = "kagent.invocation.segment"
	// AttributeDisposition records how a segment stopped when that is not
	// visible from the task state, such as an abandoned client stream.
	AttributeDisposition = "kagent.invocation.disposition"
	// AttributeInputMessages and AttributeOutputMessages carry bounded turn
	// content in the conventions' message shape, recorded only under the
	// content-capture opt-in. The truncation flags live in kagent's namespace
	// because the conventions have none, and they are siblings rather than
	// children of the message keys, because a backend that stores attributes as
	// a nested document cannot hold a string and an object at the same path:
	// ClickHouse renders both and a reader parsing the result keeps only the
	// last, dropping the captured text, and Elasticsearch rejects the mapping.
	AttributeInputMessages   = string(semconv.GenAIInputMessagesKey)
	AttributeInputTruncated  = "kagent.capture.input_truncated"
	AttributeOutputMessages  = string(semconv.GenAIOutputMessagesKey)
	AttributeOutputTruncated = "kagent.capture.output_truncated"
	// AttributeErrorType is a safe failure category. It never carries provider
	// responses, credentials, or captured content.
	AttributeErrorType = string(semconv.ErrorTypeKey)
	// AttributeLinkRelationship describes why a segment links to another span.
	AttributeLinkRelationship = "kagent.invocation.relationship"
)

// OperationInvokeAgent is the GenAI operation a native harness invocation
// span reports.
const OperationInvokeAgent = "invoke_agent"

// TransportSpanName names the wrapper span of a runtime whose invocation
// span comes from the runtime itself.
const TransportSpanName = "a2a.request"

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

// Runtime identifies the runtime that executes an agent. Its values name what
// produces the model and tool spans beneath an invocation, since that is what
// a consumer keyed on runtime needs to know, and the Go and Python ADK
// runtimes differ in what they emit.
type Runtime string

const (
	RuntimeADKGo  Runtime = "adk-go"
	RuntimeClaude Runtime = "claude"
	RuntimeCodex  Runtime = "codex"
)

// NativeHarness reports whether the runtime wraps a native coding agent whose
// model and tool spans carry no agent invocation of their own. The wrapper is
// the invoke_agent span for such a runtime. The ADK emits exactly one
// invoke_agent per turn itself, so its wrapper stays a transport span rather
// than a second invocation a consumer would count twice.
func (r Runtime) NativeHarness() bool {
	return r == RuntimeClaude || r == RuntimeCodex
}

// CaptureContentEnvironmentVariable is the OpenTelemetry GenAI instrumentation
// switch for recording prompts and responses. The controller renders it into
// every runtime from one setting, so the runtimes that read it directly and
// the ones that carry the decision in their compiled configuration agree.
const CaptureContentEnvironmentVariable = "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"

// DefaultCaptureBytes bounds captured input and output when a configuration
// enables capture without choosing a limit.
const DefaultCaptureBytes = 16 << 10

// MaxCaptureBytes is the hard ceiling on captured input and output. It bounds
// per-request memory and the payload each invocation adds to the exporter
// queue, independently of response length.
const MaxCaptureBytes = 64 << 10

// RuntimeTelemetry is the compiler-owned telemetry contract carried in a
// runtime configuration. It supplies the static identity every invocation
// span and resource needs and the content-capture policy the runtime enforces.
// Request identity is never part of it, since one runtime process serves many
// conversations, tasks, and users.
//
// A configuration without this section stays valid: the runtime keeps its
// environment-derived service identity and capture stays off.
type RuntimeTelemetry struct {
	Runtime        Runtime `json:"runtime,omitempty"`
	AgentName      string  `json:"agent_name,omitempty"`
	AgentNamespace string  `json:"agent_namespace,omitempty"`
	// Provider and Model describe the model the agent is compiled against, in
	// the GenAI conventions' vocabulary. They let a consumer attribute usage
	// that a native runtime reports without naming the model.
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	CaptureContent  bool   `json:"capture_content,omitempty"`
	MaxCaptureBytes int    `json:"max_capture_bytes,omitempty"`
}

// Validate rejects configurations a compiler could not have produced.
func (t RuntimeTelemetry) Validate() error {
	switch t.Runtime {
	case "", RuntimeADKGo, RuntimeClaude, RuntimeCodex:
	default:
		return fmt.Errorf("unsupported runtime %q", t.Runtime)
	}
	// Identity is all or nothing. A partial identity would mark a span with a
	// runtime while leaving a user-supplied agent name authoritative, since
	// Identity omits the keys it does not have.
	present := 0
	for _, value := range []string{string(t.Runtime), t.AgentName, t.AgentNamespace} {
		if value != "" {
			present++
		}
	}
	if present != 0 && present != 3 {
		return fmt.Errorf("telemetry identity requires a runtime, agent name, and namespace together")
	}
	for _, value := range []string{t.AgentName, t.AgentNamespace, t.Provider, t.Model} {
		if strings.TrimSpace(value) != value {
			return fmt.Errorf("telemetry identity must not have surrounding whitespace")
		}
	}
	if t.MaxCaptureBytes < 0 || t.MaxCaptureBytes > MaxCaptureBytes {
		return fmt.Errorf("telemetry capture limit must be between 0 and %d bytes", MaxCaptureBytes)
	}
	return nil
}

// AgentID is the namespace-qualified agent identity, or empty without one.
func (t RuntimeTelemetry) AgentID() string {
	if t.AgentName == "" || t.AgentNamespace == "" {
		return ""
	}
	return t.AgentNamespace + "/" + t.AgentName
}

// Identity returns the trusted static attributes stamped on every invocation
// span and on the runtime resource.
func (t RuntimeTelemetry) Identity() []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, 5)
	for _, entry := range []struct{ key, value string }{
		{AttributeRuntime, string(t.Runtime)},
		{AttributeAgentName, t.AgentName},
		{AttributeAgentID, t.AgentID()},
		{AttributeProviderName, t.Provider},
		{AttributeRequestModel, t.Model},
	} {
		if entry.value != "" {
			attributes = append(attributes, attribute.String(entry.key, entry.value))
		}
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

// RequestIdentity is the identity of one A2A request. It belongs on the
// invocation span, never on the process-wide resource, because one runtime
// process serves many conversations and tasks. The segment says whether the
// request starts a task or continues one that paused for input.
func RequestIdentity(conversationID, taskID string, resumed bool) []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, 3)
	if conversationID != "" {
		attributes = append(attributes, attribute.String(AttributeConversationID, conversationID))
	}
	if taskID != "" {
		attributes = append(attributes, attribute.String(AttributeTaskID, taskID))
	}
	segment := SegmentInitial
	if resumed {
		segment = SegmentResumed
	}
	return append(attributes, attribute.String(AttributeSegment, segment))
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
