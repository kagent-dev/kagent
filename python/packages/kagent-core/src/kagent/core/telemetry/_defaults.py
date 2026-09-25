"""SDK settings kagent applies when the environment leaves them unset.

The values match go/pkg/telemetry. kagent.core imports this first, since
opentelemetry.propagate reads OTEL_PROPAGATORS when it is imported.
"""

import os

DEFAULTS = {
    "OTEL_PROPAGATORS": "tracecontext",
    "OTEL_EXPORTER_OTLP_COMPRESSION": "gzip",
    "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION": "base2_exponential_bucket_histogram",
}


def apply() -> None:
    for name, value in DEFAULTS.items():
        if not os.environ.get(name):
            os.environ[name] = value


apply()
