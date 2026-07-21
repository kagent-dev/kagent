"""Outbound trace-context propagation for httpx clients.

httpx client auto-instrumentation is enabled by default, but operators can opt
out (``OTEL_INSTRUMENTATION_HTTPX_CLIENT_ENABLED=false`` /
``otel.tracing.httpxClientInstrumentation=false``) to cut span noise. When it is
disabled, nothing injects W3C trace context (``traceparent`` / ``tracestate``)
on outbound agent->controller calls, so the controller's ``/api/memories/*``
work (and any other call made through the agent's shared httpx client) starts a
*new root trace* instead of nesting under the active ``memory.read`` /
``memory.write`` span.

This module provides an httpx request event-hook that re-injects the trace
context — correlation headers only, no extra spans — restoring trace continuity
across the memory hop. It is the same remedy applied to A2A sub-agent
calls in ``_remote_a2a_tool.py``, factored out so it can be attached to any
httpx client. The A2A sub-agent hop has the same regression.
"""

import httpx
from opentelemetry.propagate import inject


async def inject_trace_context(request: httpx.Request) -> None:
    """httpx request event-hook: inject W3C trace context into outbound headers.

    ``inject()`` reads the currently-active span context and writes the
    ``traceparent`` / ``tracestate`` headers via the global textmap propagator.
    When no span is active (e.g. tracing disabled) the carrier stays empty and
    the request is left untouched, so this hook is always safe to attach.
    """
    carrier: dict[str, str] = {}
    inject(carrier)
    if carrier:
        request.headers.update(carrier)
