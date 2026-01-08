# Stub module for traceloop.sdk.tracing
# This provides a minimal no-op implementation of set_agent_name to satisfy
# the import in opentelemetry-instrumentation-openai-agents._hooks without
# pulling in the full traceloop-sdk dependency (which causes version conflicts).


def set_agent_name(name: str) -> None:
    """No-op stub for traceloop.sdk.tracing.set_agent_name."""
    pass
