"""OpenTelemetry span helpers for the kagent memory subsystem.

These spans mirror the Go ADK memory instrumentation (kagent-dev/kagent#1909,
``go/adk/pkg/telemetry/memory.go``) so both runtimes emit the same
``memory.read`` / ``memory.write`` / ``memory.consolidate`` semantic-convention
spans. Read spans are started with the caller's context so they attach as
children of the active invocation span when recall happens before LLM dispatch.

kagent emits only the operations it actually performs; governance operations
such as promote/revoke are reserved by the convention but not emitted until the
memory subsystem models them. The full governance vocabulary (memory.status,
memory.authority, memory.record_id on write) is likewise reserved but NOT
emitted here — emitting it would mean fabricating values.
"""

from contextlib import contextmanager
from typing import Iterator, Optional

from opentelemetry import trace
from opentelemetry.trace import Status, StatusCode

_TRACER = trace.get_tracer("kagent.adk.memory")

# Span names.
SPAN_MEMORY_WRITE = "memory.write"
SPAN_MEMORY_READ = "memory.read"
SPAN_MEMORY_CONSOLIDATE = "memory.consolidate"
SPAN_MEMORY_EMBED = "memory.embed"

# memory.operation values. Aligned with the memory.* semantic-convention proposal.
MEMORY_OPERATION_SAVE = "save"
MEMORY_OPERATION_LOAD = "load"
MEMORY_OPERATION_PREFETCH = "prefetch"
MEMORY_OPERATION_EXTRACT = "extract"
MEMORY_OPERATION_EMBED = "embed"

# memory.scope values. kagent scopes memory by user within an agent namespace.
MEMORY_SCOPE_USER = "user"

# memory.source values. Describes where a stored memory originated.
MEMORY_SOURCE_USER = "user"  # raw content stored verbatim
MEMORY_SOURCE_AGENT_INFERENCE = "agent_inference"  # LLM-summarized facts

# memory.injection_result values. Reports the outcome of a recall against the
# pgvector min-score gate.
MEMORY_INJECTION_INJECTED = "injected"  # at least one memory passed the threshold
MEMORY_INJECTION_FILTERED = "filtered"  # no memory passed the threshold

# Memory attribute keys.
ATTR_MEMORY_OPERATION = "memory.operation"
ATTR_MEMORY_SCOPE = "memory.scope"
ATTR_MEMORY_SOURCE = "memory.source"
ATTR_MEMORY_INDEX_REF = "memory.index_ref"
ATTR_MEMORY_INJECTION_RESULT = "memory.injection_result"
ATTR_MEMORY_ITEM_COUNT = "memory.item.count"

# Recall query-shape attributes stamped on memory.read so the span reflects the
# actual pgvector search parameters used (not just the outcome).
ATTR_MEMORY_QUERY_TOP_K = "memory.query.top_k"
ATTR_MEMORY_QUERY_MIN_SCORE = "memory.query.min_score"

# SUT (system-under-test) descriptor attributes for the memory backend.
ATTR_MEMORY_SUT_NAME = "memory.sut.name"
ATTR_MEMORY_SUT_ARCHITECTURE = "memory.sut.architecture"
ATTR_MEMORY_SUT_STORE_BACKEND = "memory.sut.store_backend"


@contextmanager
def start_memory_span(
    span_name: str,
    operation: str,
    scope: str,
    index_ref: str,
) -> Iterator[trace.Span]:
    """Start a memory.* span as a child of any span already active in context.

    Stamps the operation plus the SUT descriptor attributes that identify
    kagent's pgvector-backed memory subsystem. ``index_ref`` identifies the
    logical index the operation targets (the agent memory namespace); pass ""
    to omit. When tracing is disabled the tracer returns a no-op span.
    """
    with _TRACER.start_as_current_span(span_name) as span:
        span.set_attribute(ATTR_MEMORY_OPERATION, operation)
        span.set_attribute(ATTR_MEMORY_SUT_NAME, "kagent")
        span.set_attribute(ATTR_MEMORY_SUT_ARCHITECTURE, "vector")
        span.set_attribute(ATTR_MEMORY_SUT_STORE_BACKEND, "pgvector")
        if scope:
            span.set_attribute(ATTR_MEMORY_SCOPE, scope)
        if index_ref:
            span.set_attribute(ATTR_MEMORY_INDEX_REF, index_ref)
        yield span


@contextmanager
def start_embed_span(index_ref: str, item_count: int) -> Iterator[trace.Span]:
    """Start a memory.embed child span around embedding generation.

    Memory read/write time is dominated by vectorizing the query/content, not by
    the pgvector search or store itself. Emitting this as an explicit child of
    the active memory.* span makes that embed-vs-store/search split visible in
    the trace (previously the read/write looked like one opaque block). Stamps
    memory.operation=embed and the item count being embedded.
    """
    with _TRACER.start_as_current_span(SPAN_MEMORY_EMBED) as span:
        span.set_attribute(ATTR_MEMORY_OPERATION, MEMORY_OPERATION_EMBED)
        span.set_attribute(ATTR_MEMORY_SUT_NAME, "kagent")
        span.set_attribute(ATTR_MEMORY_SUT_ARCHITECTURE, "vector")
        span.set_attribute(ATTR_MEMORY_SUT_STORE_BACKEND, "pgvector")
        if index_ref:
            span.set_attribute(ATTR_MEMORY_INDEX_REF, index_ref)
        span.set_attribute(ATTR_MEMORY_ITEM_COUNT, item_count)
        yield span


def set_memory_read_result(span: trace.Span, count: int) -> None:
    """Stamp the recall outcome onto a memory.read span: the number of items
    returned and whether any memory passed the pgvector min-score gate
    (injected) or all were filtered out (filtered).
    """
    injection = MEMORY_INJECTION_INJECTED if count > 0 else MEMORY_INJECTION_FILTERED
    span.set_attribute(ATTR_MEMORY_ITEM_COUNT, count)
    span.set_attribute(ATTR_MEMORY_INJECTION_RESULT, injection)


def record_span_error(span: trace.Span, err: Exception) -> None:
    """Mark a span as failed and record the exception on it."""
    span.record_exception(err)
    span.set_status(Status(StatusCode.ERROR, str(err)))
