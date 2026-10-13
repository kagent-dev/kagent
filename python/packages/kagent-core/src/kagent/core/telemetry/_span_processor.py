"""Copy request identity onto every span started under a request."""

import logging

from opentelemetry import context as otel_context
from opentelemetry.sdk.trace import ReadableSpan, Span, SpanProcessor

logger = logging.getLogger(__name__)

KAGENT_ATTRIBUTES_KEY = "kagent_trace_span_attributes"


class KagentAttributesSpanProcessor(SpanProcessor):
    def on_start(self, span: Span, parent_context: otel_context.Context | None = None) -> None:
        try:
            ctx = parent_context if parent_context is not None else otel_context.get_current()
            attributes = ctx.get(KAGENT_ATTRIBUTES_KEY)
            if isinstance(attributes, dict):
                span.set_attributes({key: value for key, value in attributes.items() if value is not None})
        except Exception:
            logger.warning("Failed to add kagent attributes to span", exc_info=True)

    def on_end(self, span: ReadableSpan) -> None:
        pass

    def shutdown(self) -> None:
        pass

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return True
