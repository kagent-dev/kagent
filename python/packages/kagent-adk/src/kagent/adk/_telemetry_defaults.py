"""ADK telemetry settings kagent applies when the environment leaves them unset."""

import os

from kagent.core.telemetry import _defaults

_CAPTURE = os.environ.get("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", "").strip().upper()

os.environ.setdefault("ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN", "2")
os.environ.setdefault("OTEL_SEMCONV_STABILITY_OPT_IN", "gen_ai_latest_experimental")
# ADK records content on its own spans unless told not to.
os.environ.setdefault("ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS", str(_CAPTURE in ("SPAN_ONLY", "SPAN_AND_EVENT")).lower())
