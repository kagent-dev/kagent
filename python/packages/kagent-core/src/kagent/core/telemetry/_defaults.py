"""SDK settings kagent applies when the environment leaves them unset.

The values match go/pkg/telemetry. kagent.core imports this first, since
opentelemetry.propagate reads OTEL_PROPAGATORS when it is imported.
"""

import os

DEFAULTS = {
    "OTEL_TRACES_EXPORTER": "otlp",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_PROPAGATORS": "tracecontext",
    "OTEL_EXPORTER_OTLP_COMPRESSION": "gzip",
    "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION": "base2_exponential_bucket_histogram",
    "OTEL_SEMCONV_STABILITY_OPT_IN": "gen_ai_latest_experimental,http",
    "OTEL_INSTRUMENTATION_A2A_SDK_ENABLED": "false",
    "OTEL_PYTHON_FASTAPI_EXCLUDED_URLS": "/health,/healthz,/readyz,/thread_dump,/\\.well-known/agent-card\\.json",
}


def apply() -> None:
    for name, value in DEFAULTS.items():
        if not os.environ.get(name):
            os.environ[name] = value


apply()
