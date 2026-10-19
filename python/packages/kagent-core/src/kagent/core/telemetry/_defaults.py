"""Translate file settings into SDK defaults, preserving process OTEL overrides."""

import os
from urllib.parse import quote

from .._runtime_config import TelemetryConfig

# a2a-sdk snapshots this at import time. Kagent owns the transport spans.
os.environ["OTEL_INSTRUMENTATION_A2A_SDK_ENABLED"] = "false"


def apply(config: TelemetryConfig) -> None:
    exporters = config
    for signal in ("TRACES", "METRICS", "LOGS"):
        settings = getattr(exporters, signal.lower()) if exporters.HasField(signal.lower()) else None
        prefix = f"OTEL_EXPORTER_OTLP_{signal}_"
        enabled = (
            settings is not None or prefix + "ENDPOINT" in os.environ or "OTEL_EXPORTER_OTLP_ENDPOINT" in os.environ
        )
        os.environ.setdefault(f"OTEL_{signal}_EXPORTER", "otlp" if enabled else "none")
        if settings is not None:
            # An explicit connection setting replaces the file's entire pair.
            # Let the SDK default a missing endpoint or protocol.
            if not any(
                key in os.environ
                for key in (
                    prefix + "ENDPOINT",
                    prefix + "PROTOCOL",
                    "OTEL_EXPORTER_OTLP_ENDPOINT",
                    "OTEL_EXPORTER_OTLP_PROTOCOL",
                )
            ):
                os.environ[prefix + "ENDPOINT"] = settings.endpoint
                os.environ[prefix + "PROTOCOL"] = settings.protocol or "grpc"
            timeout_ms = settings.timeout.ToMilliseconds() if settings.HasField("timeout") else 0
            if timeout_ms not in (0, 10000) and "OTEL_EXPORTER_OTLP_TIMEOUT" not in os.environ:
                os.environ.setdefault(prefix + "TIMEOUT", str(timeout_ms))
    attributes = [f"{key}={quote(value, safe='')}" for key, value in sorted(exporters.resource_attributes.items())]
    if attributes:
        attributes.append(os.environ.get("OTEL_RESOURCE_ATTRIBUTES", ""))
        os.environ["OTEL_RESOURCE_ATTRIBUTES"] = ",".join(filter(None, attributes))
    for name, value in {
        "OTEL_PROPAGATORS": "tracecontext",
        "ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN": "2",
        "ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS": "false",
        "OTEL_EXPORTER_OTLP_COMPRESSION": "gzip",
        "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION": "base2_exponential_bucket_histogram",
        "OTEL_SEMCONV_STABILITY_OPT_IN": "gen_ai_latest_experimental,http",
        "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT": "SPAN_ONLY" if config.capture_content else "NO_CONTENT",
        "OTEL_PYTHON_FASTAPI_EXCLUDED_URLS": r"/health,/healthz,/readyz,/thread_dump,/\.well-known/agent-card\.json",
    }.items():
        os.environ.setdefault(name, value)
    os.environ.update(
        {
            "OTEL_INSTRUMENTATION_A2A_SDK_ENABLED": "false",
            "CREWAI_DISABLE_TELEMETRY": "true",
        }
    )
