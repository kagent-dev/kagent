import importlib
import os

import pytest

from kagent.adk import _telemetry_defaults


@pytest.mark.parametrize(("capture", "expected"), [("SPAN_ONLY", "true"), ("NO_CONTENT", "false"), (None, "false")])
def test_adk_span_content_follows_the_capture_setting(monkeypatch, capture, expected):
    monkeypatch.delenv("ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS", raising=False)
    monkeypatch.delenv("ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN", raising=False)
    monkeypatch.delenv("OTEL_SEMCONV_STABILITY_OPT_IN", raising=False)
    if capture is None:
        monkeypatch.delenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", raising=False)
    else:
        monkeypatch.setenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", capture)

    importlib.reload(_telemetry_defaults)

    assert os.environ["ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS"] == expected
    assert os.environ["ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN"] == "2"
    assert os.environ["OTEL_SEMCONV_STABILITY_OPT_IN"] == "gen_ai_latest_experimental"


def test_explicit_adk_settings_win(monkeypatch):
    monkeypatch.setenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", "SPAN_ONLY")
    monkeypatch.setenv("ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS", "false")

    importlib.reload(_telemetry_defaults)

    assert os.environ["ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS"] == "false"
