import asyncio
import logging
import os

from fastapi import FastAPI
from opentelemetry import _logs, metrics, trace
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.propagate import set_global_textmap
from opentelemetry.propagators.composite import CompositePropagator
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import OTELResourceDetector, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.util._importlib_metadata import entry_points

from ..telemetry import _defaults
from ._span_processor import KagentAttributesSpanProcessor


def _resolve_otlp_protocol(signal: str) -> str:
    """Resolve the OTLP protocol from signal-specific or general env vars.

    Follows the OpenTelemetry specification precedence:
    signal-specific (e.g. OTEL_EXPORTER_OTLP_TRACES_PROTOCOL) > general > default (grpc).
    """
    raw = os.getenv(f"OTEL_EXPORTER_OTLP_{signal}_PROTOCOL") or os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL") or "grpc"
    return raw.strip().lower()


def _create_span_exporter(**kwargs):
    """Create an OTLPSpanExporter using the protocol from env vars."""
    protocol = _resolve_otlp_protocol("TRACES")
    if protocol == "http/protobuf":
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
    else:
        from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
    logging.info("Using %s protocol for trace exporter", protocol)
    return OTLPSpanExporter(**kwargs)


def _create_log_exporter(**kwargs):
    """Create an OTLPLogExporter using the protocol from env vars."""
    protocol = _resolve_otlp_protocol("LOGS")
    if protocol == "http/protobuf":
        from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
    else:
        from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
    logging.info("Using %s protocol for log exporter", protocol)
    return OTLPLogExporter(**kwargs)


def _create_metric_exporter(**kwargs):
    """Create an OTLPMetricExporter using the protocol from env vars."""
    protocol = _resolve_otlp_protocol("METRICS")
    if protocol == "http/protobuf":
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
    else:
        from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
    return OTLPMetricExporter(**kwargs)


def _resolve_otlp_timeout_seconds(signal: str) -> float:
    """
    Resolve OTLP timeout env vars (milliseconds) into seconds for exporters.
    By default, Python OTLP exporter reads timeout env var as seconds.
    However, OTEL spec defines timeout as milliseconds.
    """
    signal_timeout_env = f"OTEL_EXPORTER_OTLP_{signal}_TIMEOUT"
    raw_timeout = os.getenv(signal_timeout_env) or os.getenv("OTEL_EXPORTER_OTLP_TIMEOUT")
    if raw_timeout is None:
        # OTEL spec default is 10000ms
        return 10.0

    try:
        timeout_millis = float(raw_timeout)
    except ValueError:
        logging.warning(
            "Invalid OTEL timeout value %r from %s; falling back to 10000ms",
            raw_timeout,
            signal_timeout_env,
        )
        return 10.0

    if timeout_millis < 0:
        logging.warning(
            "Negative OTEL timeout value %r from %s; falling back to 10000ms",
            raw_timeout,
            signal_timeout_env,
        )
        return 10.0

    return timeout_millis / 1000.0


def _instrument_anthropic(logger_provider=None):
    """Instrument Anthropic SDK if available."""
    try:
        from opentelemetry.instrumentation.anthropic import AnthropicInstrumentor

        if logger_provider:
            AnthropicInstrumentor(use_legacy_attributes=False).instrument(logger_provider=logger_provider)
        else:
            AnthropicInstrumentor().instrument()
    except ImportError:
        # Anthropic SDK is not installed; skipping instrumentation.
        pass


def _instrument_google_generativeai(logger_provider=None):
    """Instrument Google GenerativeAI SDK if available."""
    try:
        from opentelemetry.instrumentation.google_generativeai import GoogleGenerativeAiInstrumentor

        if logger_provider:
            GoogleGenerativeAiInstrumentor(use_legacy_attributes=False).instrument(logger_provider=logger_provider)
        else:
            GoogleGenerativeAiInstrumentor().instrument()
    except ImportError:
        # Google GenerativeAI SDK is not installed; skipping instrumentation.
        pass


# OTEL_BSP_EXPORT_TIMEOUT defaults to 30s and must not bound a response tail.
FLUSH_TIMEOUT_MILLIS = 3000


def signal_enabled(signal: str) -> bool:
    """Report whether a signal exports, reading unset as otlp like the SDK."""
    if os.getenv("OTEL_SDK_DISABLED", "").strip().lower() == "true":
        return False
    exporters = os.getenv(f"OTEL_{signal}_EXPORTER") or "otlp"
    return "otlp" in (exporter.strip().lower() for exporter in exporters.split(","))


def _environment_propagator() -> CompositePropagator:
    """Build the propagator OTEL_PROPAGATORS names, even when opentelemetry.propagate was imported first."""
    names = [name.strip() for name in os.environ.get("OTEL_PROPAGATORS", "").split(",") if name.strip()]
    if "none" in names:
        return CompositePropagator([])
    return CompositePropagator(
        [next(iter(entry_points(group="opentelemetry_propagator", name=name))).load()() for name in names]
    )


def force_flush(timeout_millis: int = FLUSH_TIMEOUT_MILLIS) -> None:
    """Export any spans and metrics still buffered in their providers.

    Call before a response completes when the process may be suspended right
    afterwards: Agent Substrate checkpoints the actor as soon as the A2A
    response body closes, so unexported spans stay frozen in the snapshot
    until the session's next resume (or forever, for a session's last
    message). No-op for a provider without force_flush (signal disabled).
    """
    for provider in (trace.get_tracer_provider(), metrics.get_meter_provider()):
        flush = getattr(provider, "force_flush", None)
        if flush is None:
            continue
        try:
            flush(timeout_millis)
        except Exception:
            logging.warning("Failed to flush pending telemetry", exc_info=True)


# High-frequency probe endpoints with nothing worth flushing.
_FLUSH_EXCLUDED_PATHS = frozenset({"/health", "/healthz", "/thread_dump"})


def _should_flush(scope) -> bool:
    if scope.get("type") != "http":
        return False
    path = scope.get("path", "")
    return path not in _FLUSH_EXCLUDED_PATHS and not path.endswith("/.well-known/agent-card.json")


def _add_post_response_flush(app: FastAPI) -> None:
    """Flush buffered spans before each response completes: on Agent Substrate
    the actor is checkpointed as soon as the response body closes, discarding
    any unexported spans when the session uses a data-only snapshot.

    Hold the terminal ASGI message outside the OTel middleware. This lets OTel
    end the inbound server span, then flushes before the client receives the
    response terminator and triggers suspension.
    """
    inner_build = app.build_middleware_stack

    def build_middleware_stack():
        inner = inner_build()

        async def flushing_app(scope, receive, send):
            if not _should_flush(scope):
                await inner(scope, receive, send)
                return

            terminal_message = None
            expecting_trailers = False

            async def hold_terminal_message(message):
                nonlocal expecting_trailers, terminal_message
                message_type = message.get("type")
                if message_type == "http.response.start":
                    expecting_trailers = message.get("trailers", False)
                is_terminal = (
                    message_type == "http.response.body"
                    and not expecting_trailers
                    and not message.get("more_body", False)
                ) or (message_type == "http.response.trailers" and not message.get("more_trailers", False))
                if is_terminal:
                    terminal_message = message
                else:
                    await send(message)

            try:
                await inner(scope, receive, hold_terminal_message)
            finally:
                # Always forward a held terminal message — even when the app
                # raises after producing it — or the client never receives the
                # response terminator.
                if terminal_message is not None:
                    try:
                        await asyncio.to_thread(force_flush)
                    finally:
                        await send(terminal_message)

        return flushing_app

    app.build_middleware_stack = build_middleware_stack


def configure(
    name: str = "kagent",
    namespace: str = "kagent",
    fastapi_app: FastAPI | None = None,
    instrument_openai_client: bool = True,
):
    """Configure OpenTelemetry tracing and logging for this service.

    This sets up OpenTelemetry providers and exporters for tracing and logging,
    using environment variables to determine whether each is enabled.

    Args:
        name: ``service.name`` when OTEL_SERVICE_NAME does not set one. Default is "kagent".
        namespace: ``service.namespace`` when OTEL_RESOURCE_ATTRIBUTES does not set one. Default is "kagent".
        fastapi_app: Optional FastAPI application instance to instrument. If
            provided and tracing is enabled, FastAPI routes will be instrumented.
        instrument_openai_client: Install the low-level ``OpenAIInstrumentor``. Set
            ``False`` when a higher-level instrumentor already covers OpenAI (e.g. the
            Agents SDK's ``OpenAIAgentsInstrumentor``); double-wrapping the OpenAI SDK
            breaks the Agents SDK streaming Responses path.
    """
    _defaults.apply()
    set_global_textmap(_environment_propagator())
    tracing_enabled = signal_enabled("TRACES")
    metrics_enabled = signal_enabled("METRICS")
    logging_enabled = signal_enabled("LOGS")

    # Resource.create lets the attributes it is given win, so the environment is
    # passed over the defaults to keep OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES authoritative.
    environment = OTELResourceDetector().detect().attributes
    resource = Resource.create({"service.name": name, "service.namespace": namespace, **environment})

    # Configure tracing if enabled
    if tracing_enabled:
        logging.info("Enabling tracing")
        # The exporter reads its endpoint itself, which adds /v1/traces to a
        # shared http/protobuf endpoint.
        processor = BatchSpanProcessor(_create_span_exporter(timeout=_resolve_otlp_timeout_seconds("TRACES")))

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

        # Exclude agent-card endpoint from traces — this is used as a health
        # check endpoint (high-frequency polling requests) and has little
        # diagnostic value. Inbound only: HTTPXClientInstrumentor accepts no
        # excluded_urls kwarg (newer releases read OTEL_PYTHON_HTTPX_EXCLUDED_URLS
        # instead), so passing one here was silently dropped.
        _excluded_urls = ".*/\\.well-known/agent-card\\.json"
        HTTPXClientInstrumentor().instrument()
        if fastapi_app:
            FastAPIInstrumentor().instrument_app(fastapi_app, excluded_urls=_excluded_urls)
    if metrics_enabled:
        reader = PeriodicExportingMetricReader(
            _create_metric_exporter(timeout=_resolve_otlp_timeout_seconds("METRICS"))
        )
        metrics.set_meter_provider(MeterProvider(resource=resource, metric_readers=[reader]))
        logging.info("Meter provider configured with OTLP")
    if fastapi_app and (tracing_enabled or metrics_enabled):
        _add_post_response_flush(fastapi_app)
    # Configure logging if enabled
    if logging_enabled:
        logging.info("Enabling logging for GenAI events")
        logger_provider = LoggerProvider(resource=resource)
        log_processor = BatchLogRecordProcessor(_create_log_exporter(timeout=_resolve_otlp_timeout_seconds("LOGS")))
        logger_provider.add_log_record_processor(log_processor)

        _logs.set_logger_provider(logger_provider)
        logging.info("Log provider configured with OTLP")
        # When logging is enabled, use new event-based approach (input/output as log events in Body)
        logging.info("OpenAI instrumentation configured with event logging capability")
        if instrument_openai_client:
            OpenAIInstrumentor(use_legacy_attributes=False).instrument(logger_provider=logger_provider)
        _instrument_anthropic(logger_provider)
        _instrument_google_generativeai(logger_provider)
    elif tracing_enabled:
        # Use legacy attributes (input/output as GenAI span attributes)
        logging.info("OpenAI instrumentation configured with legacy GenAI span attributes")
        if instrument_openai_client:
            OpenAIInstrumentor().instrument()
        _instrument_anthropic()
        _instrument_google_generativeai()
    # Neither signal enabled: skip GenAI instrumentation so telemetry has no runtime side effects.
