import logging
import os

from fastapi import FastAPI
from opentelemetry import _logs, trace
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.anthropic import AnthropicInstrumentor
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.sdk._events import EventLoggerProvider
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor


def configure(fastapi_app: FastAPI | None = None):
    tracing_enabled = os.getenv("OTEL_TRACING_ENABLED", "false").lower() == "true"
    logging_enabled = os.getenv("OTEL_LOGGING_ENABLED", "false").lower() == "true"

    resource = Resource({"service.name": "kagent"})

    # Configure tracing if enabled
    if tracing_enabled:
        logging.info("Enabling tracing")
        tracer_provider = TracerProvider(resource=resource)
        # Check new env var first, fall back to old one for backward compatibility
        trace_endpoint = os.getenv("OTEL_TRACING_EXPORTER_OTLP_ENDPOINT") or os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
        if trace_endpoint:
            processor = BatchSpanProcessor(OTLPSpanExporter(endpoint=trace_endpoint))
        else:
            processor = BatchSpanProcessor(OTLPSpanExporter())
        tracer_provider.add_span_processor(processor)
        trace.set_tracer_provider(tracer_provider)
        HTTPXClientInstrumentor().instrument()
        if fastapi_app:
            FastAPIInstrumentor().instrument_app(fastapi_app)
    # Configure logging if enabled
    if logging_enabled:
        logging.info("Enabling logging for GenAI events")
        logger_provider = LoggerProvider(resource=resource)
        log_endpoint = os.getenv("OTEL_LOGGING_EXPORTER_OTLP_ENDPOINT")
        logging.info(f"Log endpoint configured: {log_endpoint}")

        # Add OTLP exporter
        if log_endpoint:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter(endpoint=log_endpoint))
        else:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter())
        logger_provider.add_log_record_processor(log_processor)

        _logs.set_logger_provider(logger_provider)
        logging.info("Log provider configured with OTLP")
        # When logging is enabled, use new event-based approach (input/output as log events in Body)
        logging.info("OpenAI instrumentation configured with event logging capability")
        # Create event logger provider using the configured logger provider
        event_logger_provider = EventLoggerProvider(logger_provider)
        OpenAIInstrumentor(use_legacy_attributes=False).instrument(event_logger_provider=event_logger_provider)
        AnthropicInstrumentor(use_legacy_attributes=False).instrument(event_logger_provider=event_logger_provider)
    else:
        # Use legacy attributes (input/output as GenAI span attributes)
        logging.info("OpenAI instrumentation configured with legacy GenAI span attributes")
        OpenAIInstrumentor().instrument()
        AnthropicInstrumentor().instrument()
