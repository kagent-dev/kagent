"""Custom span processor to add kagent attributes to all spans in a request context."""

import logging
from contextvars import Token
from typing import Optional

from opentelemetry import context as otel_context
from opentelemetry.sdk.trace import ReadableSpan, Span, SpanProcessor

logger = logging.getLogger(__name__)

KAGENT_ATTRIBUTES_KEY = "kagent_trace_span_attributes"


class KagentAttributesSpanProcessor(SpanProcessor):
    """A SpanProcessor that adds kagent-specific attributes to all spans."""

    def on_start(self, span: Span, parent_context: Optional[otel_context.Context] = None) -> None:
        """Called when a span is started. Adds kagent attributes if present in context."""
        try:
            ctx = parent_context if parent_context is not None else otel_context.get_current()
            attributes = ctx.get(KAGENT_ATTRIBUTES_KEY)

            if attributes and isinstance(attributes, dict):
                for key, value in attributes.items():
                    if value is not None:
                        span.set_attribute(key, value)
        except Exception as e:
            logger.warning(f"Failed to add kagent attributes to span: {e}")

    def on_end(self, span: ReadableSpan) -> None:
        """Called when a span is ended. No action needed."""
        pass

    def shutdown(self) -> None:
        """Called when the tracer provider is shutdown."""
        pass

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        """Force flush any buffered spans. No buffering in this processor."""
        return True


def set_kagent_span_attributes(attributes: dict) -> Token[otel_context.Context]:
    """Set kagent span attributes in the context.
    Args:
        attributes: Dictionary of kagent span attributes to store in context
    Returns:
        A context token that can be used to detach the context
    """
    return otel_context.attach(otel_context.set_value(KAGENT_ATTRIBUTES_KEY, attributes))


def clear_kagent_span_attributes(token: Token[otel_context.Context]) -> None:
    """Clear kagent span attributes from the OpenTelemetry context.
    Args:
        token: The context token returned by set_kagent_span_attributes
    """
    otel_context.detach(token)
