import importlib
import os

import pytest

from kagent.adk import _telemetry_defaults


@pytest.mark.parametrize("capture", ["SPAN_ONLY", "NO_CONTENT", None])
def test_adk_legacy_span_content_stays_off_whatever_the_capture_setting(monkeypatch, capture):
    monkeypatch.delenv("ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS", raising=False)
    monkeypatch.delenv("ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN", raising=False)
    if capture is None:
        monkeypatch.delenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", raising=False)
    else:
        monkeypatch.setenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", capture)

    importlib.reload(_telemetry_defaults)

    assert os.environ["ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS"] == "false"
    assert os.environ["ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN"] == "2"


def test_explicit_adk_settings_win(monkeypatch):
    monkeypatch.setenv("ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS", "true")

    importlib.reload(_telemetry_defaults)

    assert os.environ["ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS"] == "true"
