package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Memory span names.
//
// These spans sit alongside the existing gen_ai.* spans on invoke_agent and give
// operators dedicated visibility into the memory subsystem. Read spans (memory.read)
// are started with the caller's context so they attach as children of the active
// invocation span when recall happens before LLM dispatch, keeping the trace tree
// connected.
const (
	SpanMemoryWrite       = "memory.write"
	SpanMemoryRead        = "memory.read"
	SpanMemoryConsolidate = "memory.consolidate"
)

// MemoryOperation values. Aligned with the memory.* semantic-convention proposal
// in kagent-dev/kagent#1909. kagent emits only the operations it actually performs;
// governance operations such as promote/revoke are reserved by the convention but
// not emitted until the memory subsystem models them.
const (
	MemoryOperationSave     = "save"
	MemoryOperationLoad     = "load"
	MemoryOperationPrefetch = "prefetch"
	MemoryOperationExtract  = "extract"
)

// MemoryScope values. kagent scopes memory by user within an agent namespace.
const (
	MemoryScopeUser = "user"
)

// MemorySource values. Describes where a stored memory originated.
const (
	MemorySourceUser           = "user"            // raw session content
	MemorySourceAgentInference = "agent_inference" // LLM-summarized facts
)

// MemoryInjectionResult values. Reports the outcome of a recall against the
// pgvector min-score gate.
const (
	MemoryInjectionInjected = "injected" // at least one memory passed the threshold and was returned
	MemoryInjectionFiltered = "filtered" // no memory passed the threshold
)

// Memory attribute keys.
//
// The full governance vocabulary (memory.status, memory.authority, memory.record_id
// on write) is defined by the convention but intentionally NOT emitted here: kagent's
// memory subsystem does not yet model lifecycle status or injection authority, so
// emitting them would mean fabricating values. They are reserved for when kagent gains
// a memory governance model.
const (
	AttrMemoryOperation       = "memory.operation"
	AttrMemoryScope           = "memory.scope"
	AttrMemorySource          = "memory.source"
	AttrMemoryIndexRef        = "memory.index_ref"
	AttrMemoryInjectionResult = "memory.injection_result"
	AttrMemoryItemCount       = "memory.item.count"

	// SUT (system-under-test) descriptor attributes for the memory backend.
	AttrMemorySUTName         = "memory.sut.name"
	AttrMemorySUTArchitecture = "memory.sut.architecture"
	AttrMemorySUTStoreBackend = "memory.sut.store_backend"
)

const memoryTracerName = "github.com/kagent-dev/kagent/go/adk/pkg/memory"

// StartMemorySpan starts a memory.* span as a child of any span already present in
// ctx (for example invoke_agent), stamping the operation plus the SUT descriptor
// attributes that identify kagent's pgvector-backed memory subsystem. indexRef
// identifies the logical index the operation targets (the agent memory namespace);
// pass "" to omit. When telemetry is disabled the returned span is a no-op.
func StartMemorySpan(ctx context.Context, spanName, operation, scope, indexRef string) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(memoryTracerName).Start(ctx, spanName)
	span.SetAttributes(
		attribute.String(AttrMemoryOperation, operation),
		attribute.String(AttrMemorySUTName, "kagent"),
		attribute.String(AttrMemorySUTArchitecture, "vector"),
		attribute.String(AttrMemorySUTStoreBackend, "pgvector"),
	)
	if scope != "" {
		span.SetAttributes(attribute.String(AttrMemoryScope, scope))
	}
	if indexRef != "" {
		span.SetAttributes(attribute.String(AttrMemoryIndexRef, indexRef))
	}
	return ctx, span
}
