import logging
import os

from fastapi import FastAPI
from opentelemetry import _logs, metrics, trace
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.sdk._events import EventLoggerProvider
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from ._span_processor import KagentAttributesSpanProcessor


def _instrument_anthropic(event_logger_provider=None):
    """Instrument Anthropic SDK if available."""
    try:
        from opentelemetry.instrumentation.anthropic import AnthropicInstrumentor

        if event_logger_provider:
            AnthropicInstrumentor(use_legacy_attributes=False).instrument(event_logger_provider=event_logger_provider)
        else:
            AnthropicInstrumentor().instrument()
    except ImportError:
        # Anthropic SDK is not installed; skipping instrumentation.
        pass


def _instrument_google_generativeai():
    """Instrument Google GenerativeAI SDK if available."""
    try:
        from opentelemetry.instrumentation.google_generativeai import GoogleGenerativeAiInstrumentor

        GoogleGenerativeAiInstrumentor().instrument()
    except ImportError:
        # Google GenerativeAI SDK is not installed; skipping instrumentation.
        pass


def configure(name: str = "kagent", namespace: str = "kagent", fastapi_app: FastAPI | None = None):
    """Configure OpenTelemetry tracing, logging, and metrics for this service.

    This sets up OpenTelemetry providers and exporters for tracing, logging,
    and metrics, using environment variables to determine whether each is enabled.

    Providers are configured before instrumentors so that instrumentors can
    discover and use all available providers (TracerProvider, MeterProvider, etc.).

    Args:
        name: service name to report to OpenTelemetry (used as ``service.name``). Default is "kagent".
        namespace: logical namespace for the service (used as ``service.namespace``). Default is "kagent".
        fastapi_app: Optional FastAPI application instance to instrument. If
            provided and tracing is enabled, FastAPI routes will be instrumented.
            If metrics is enabled, a ``/metrics`` endpoint will be added for
            Prometheus scraping.
    """
    tracing_enabled = os.getenv("OTEL_TRACING_ENABLED", "false").lower() == "true"
    logging_enabled = os.getenv("OTEL_LOGGING_ENABLED", "false").lower() == "true"
    metrics_enabled = os.getenv("OTEL_METRICS_ENABLED", "false").lower() == "true"

    resource = Resource({"service.name": name, "service.namespace": namespace})

    # ------------------------------------------------------------------ #
    # 1. Configure providers BEFORE instrumentors so that instrumentors   #
    #    can discover MeterProvider, TracerProvider, etc. at init time.    #
    # ------------------------------------------------------------------ #

    # 1a. Metrics provider (Prometheus pull endpoint)
    if metrics_enabled:
        logging.info("Enabling Prometheus metrics")
        try:
            from opentelemetry.exporter.prometheus import PrometheusMetricReader
            from opentelemetry.sdk.metrics import MeterProvider

            reader = PrometheusMetricReader()
            meter_provider = MeterProvider(resource=resource, metric_readers=[reader])
            metrics.set_meter_provider(meter_provider)
            logging.info("MeterProvider configured with Prometheus exporter")

            if fastapi_app:
                from prometheus_client import CONTENT_TYPE_LATEST, generate_latest
                from starlette.responses import Response

                @fastapi_app.get("/metrics")
                async def metrics_endpoint():
                    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)

                logging.info("Added /metrics endpoint for Prometheus scraping")
        except ImportError:
            logging.warning(
                "opentelemetry-exporter-prometheus is not installed; "
                "metrics endpoint will not be available. "
                "Install it with: pip install opentelemetry-exporter-prometheus"
            )

    # 1b. Tracing provider
    if tracing_enabled:
        logging.info("Enabling tracing")
        # Check standard OTEL env vars: signal-specific endpoint first, then general endpoint
        trace_endpoint = (
            os.getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
            or os.getenv("OTEL_TRACING_EXPORTER_OTLP_ENDPOINT")  # Backward compatibility
            or os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
        )
        logging.info("Trace endpoint: %s", trace_endpoint or "<default>")
        if trace_endpoint:
            processor = BatchSpanProcessor(OTLPSpanExporter(endpoint=trace_endpoint))
        else:
            processor = BatchSpanProcessor(OTLPSpanExporter())

        # Check if a TracerProvider already exists (e.g., set by CrewAI)
        current_provider = trace.get_tracer_provider()
        if isinstance(current_provider, TracerProvider):
            # TracerProvider already exists, just add our processors to it
            current_provider.add_span_processor(processor)
            current_provider.add_span_processor(KagentAttributesSpanProcessor())
            logging.info("Added OTLP processors to existing TracerProvider")
        else:
            # No provider set, create new one
            tracer_provider = TracerProvider(resource=resource)
            tracer_provider.add_span_processor(processor)
            tracer_provider.add_span_processor(KagentAttributesSpanProcessor())
            trace.set_tracer_provider(tracer_provider)
            logging.info("Created new TracerProvider")

    # 1c. Logging provider
    event_logger_provider = None
    if logging_enabled:
        logging.info("Enabling logging for GenAI events")
        logger_provider = LoggerProvider(resource=resource)
        # Check standard OTEL env vars: signal-specific endpoint first, then general endpoint
        log_endpoint = (
            os.getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
            or os.getenv("OTEL_LOGGING_EXPORTER_OTLP_ENDPOINT")  # Backward compatibility
            or os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
        )
        logging.info("Log endpoint: %s", log_endpoint or "<default>")

        # Add OTLP exporter
        if log_endpoint:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter(endpoint=log_endpoint))
        else:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter())
        logger_provider.add_log_record_processor(log_processor)

        _logs.set_logger_provider(logger_provider)
        logging.info("Log provider configured with OTLP")
        # Create event logger provider for instrumentors
        event_logger_provider = EventLoggerProvider(logger_provider)

    # ------------------------------------------------------------------ #
    # 2. Instrument libraries — all providers are now available.          #
    # ------------------------------------------------------------------ #

    if tracing_enabled:
        HTTPXClientInstrumentor().instrument()
        if fastapi_app:
            FastAPIInstrumentor().instrument_app(fastapi_app)

    if event_logger_provider:
        # Event logging mode: input/output as log events in Body
        logging.info("OpenAI instrumentation configured with event logging capability")
        OpenAIInstrumentor(use_legacy_attributes=False).instrument(event_logger_provider=event_logger_provider)
        _instrument_anthropic(event_logger_provider)
    else:
        # Legacy attributes mode: input/output as GenAI span attributes
        logging.info("OpenAI instrumentation configured with legacy GenAI span attributes")
        OpenAIInstrumentor().instrument()
        _instrument_anthropic()
        _instrument_google_generativeai()

    # ------------------------------------------------------------------ #
    # 3. LiteLLM metrics callback for providers that bypass their SDK.   #
    #    LiteLLM uses raw httpx for some providers (e.g., Anthropic),     #
    #    so the SDK instrumentors never fire. This callback fills the gap.#
    # ------------------------------------------------------------------ #

    if metrics_enabled:
        _register_litellm_metrics_callback()


def _register_litellm_metrics_callback():
    """Register a LiteLLM callback that records GenAI metrics for providers
    where LiteLLM bypasses the provider's Python SDK (e.g., Anthropic).

    LiteLLM uses raw httpx POST requests for some providers instead of their
    official Python SDKs. This means the OpenTelemetry instrumentors for those
    SDKs never fire and no metrics are recorded. This callback fills that gap
    by recording metrics directly from LiteLLM's success/failure callbacks.

    Providers where LiteLLM uses the SDK directly (e.g., OpenAI) are skipped
    to avoid double-counting with the existing instrumentor metrics.
    """
    try:
        import litellm
        from litellm.integrations.custom_logger import CustomLogger
    except ImportError:
        logging.debug("litellm not installed; skipping LiteLLM metrics callback")
        return

    meter = metrics.get_meter("kagent.litellm")
    token_histogram = meter.create_histogram(
        name="gen_ai.client.token.usage",
        unit="token",
        description="Measures number of input and output tokens used",
    )
    duration_histogram = meter.create_histogram(
        name="gen_ai.client.operation.duration",
        unit="s",
        description="GenAI operation duration",
    )

    # Providers where LiteLLM uses the Python SDK directly, so the
    # SDK instrumentor already captures metrics. Skip these to avoid
    # double-counting.
    SDK_INSTRUMENTED_PROVIDERS = frozenset(
        {
            "openai",
            "azure",
            "azure_text",
            "azure_ai",
        }
    )

    class _MetricsCallback(CustomLogger):
        def _record_metrics(self, kwargs, response_obj, start_time, end_time):
            provider = kwargs.get("custom_llm_provider", "")
            if provider in SDK_INSTRUMENTED_PROVIDERS:
                return

            model = kwargs.get("model", "unknown")
            stream = kwargs.get("stream", False)
            # Match attribute names used by the OpenAI instrumentor
            # so all providers appear with consistent labels in Prometheus.
            base_attrs = {
                "gen_ai.system": provider or "unknown",
                "gen_ai.response.model": model,
                "gen_ai.operation.name": "chat",
                "server.address": "",
                "stream": stream,
            }

            duration_s = (end_time - start_time).total_seconds()
            duration_histogram.record(duration_s, attributes=base_attrs)

            usage = getattr(response_obj, "usage", None)
            if usage is None and isinstance(response_obj, dict):
                usage = response_obj.get("usage")
            if usage is None:
                return

            input_tokens = getattr(usage, "prompt_tokens", None)
            if input_tokens is None and isinstance(usage, dict):
                input_tokens = usage.get("prompt_tokens", 0)
            output_tokens = getattr(usage, "completion_tokens", None)
            if output_tokens is None and isinstance(usage, dict):
                output_tokens = usage.get("completion_tokens", 0)

            if input_tokens:
                token_histogram.record(
                    input_tokens,
                    attributes={**base_attrs, "gen_ai.token.type": "input"},
                )
            if output_tokens:
                token_histogram.record(
                    output_tokens,
                    attributes={**base_attrs, "gen_ai.token.type": "output"},
                )

        def log_success_event(self, kwargs, response_obj, start_time, end_time):
            try:
                self._record_metrics(kwargs, response_obj, start_time, end_time)
            except Exception:
                logging.debug("Failed to record LiteLLM metrics", exc_info=True)

        async def async_log_success_event(self, kwargs, response_obj, start_time, end_time):
            self.log_success_event(kwargs, response_obj, start_time, end_time)

    litellm.callbacks.append(_MetricsCallback())
    logging.info("Registered LiteLLM metrics callback for non-SDK providers")
