"""Tests for the after_tool_callback that records GenAI tool-call attributes."""

from __future__ import annotations

import json
from types import SimpleNamespace

import pytest
from google.adk.agents.run_config import RunConfig
from google.adk.telemetry.context import ContentCapturingMode, TelemetryConfig
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from kagent.adk._tool_span import (
    GEN_AI_TOOL_CALL_ARGUMENTS,
    GEN_AI_TOOL_CALL_RESULT,
    make_tool_span_attributes_callback,
)


@pytest.fixture
def tracing() -> tuple[InMemorySpanExporter, TracerProvider]:
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    return exporter, provider


def _tool_context(telemetry: TelemetryConfig | None = None):
    run_config = RunConfig(telemetry=telemetry) if telemetry is not None else RunConfig()
    return SimpleNamespace(_invocation_context=SimpleNamespace(run_config=run_config))


def _run_callback(provider: TracerProvider, tool_context, args, tool_response):
    callback = make_tool_span_attributes_callback()
    tool = SimpleNamespace(name="get_pods")
    with provider.get_tracer("test").start_as_current_span("execute_tool get_pods"):
        assert callback(tool=tool, args=args, tool_context=tool_context, tool_response=tool_response) is None


OTEL_CAPTURE_ENV = "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"


def test_records_arguments_and_result(tracing, monkeypatch):
    monkeypatch.setenv(OTEL_CAPTURE_ENV, "SPAN_ONLY")
    exporter, provider = tracing
    _run_callback(provider, _tool_context(), {"namespace": "default"}, {"pods": ["a", "b"]})

    (span,) = exporter.get_finished_spans()
    assert json.loads(span.attributes[GEN_AI_TOOL_CALL_ARGUMENTS]) == {"namespace": "default"}
    assert json.loads(span.attributes[GEN_AI_TOOL_CALL_RESULT]) == {"pods": ["a", "b"]}


def test_wraps_non_dict_result(tracing, monkeypatch):
    monkeypatch.setenv(OTEL_CAPTURE_ENV, "SPAN_AND_EVENT")
    exporter, provider = tracing
    _run_callback(provider, _tool_context(), {}, "plain text")

    (span,) = exporter.get_finished_spans()
    assert json.loads(span.attributes[GEN_AI_TOOL_CALL_RESULT]) == {"result": "plain text"}


@pytest.mark.parametrize("mode", [None, "NO_CONTENT", "EVENT_ONLY", "true"])
def test_emits_empty_payload_unless_content_is_routed_to_spans(tracing, monkeypatch, mode):
    if mode is None:
        monkeypatch.delenv(OTEL_CAPTURE_ENV, raising=False)
    else:
        monkeypatch.setenv(OTEL_CAPTURE_ENV, mode)
    exporter, provider = tracing
    _run_callback(provider, _tool_context(), {"namespace": "default"}, {"pods": 2})

    (span,) = exporter.get_finished_spans()
    assert span.attributes[GEN_AI_TOOL_CALL_ARGUMENTS] == "{}"
    assert span.attributes[GEN_AI_TOOL_CALL_RESULT] == "{}"


def test_honours_per_request_telemetry_config(tracing, monkeypatch):
    monkeypatch.setenv(OTEL_CAPTURE_ENV, "SPAN_ONLY")
    exporter, provider = tracing
    _run_callback(
        provider,
        _tool_context(TelemetryConfig(capture_message_content=ContentCapturingMode.NO_CONTENT)),
        {"namespace": "default"},
        {"pods": 2},
    )

    (span,) = exporter.get_finished_spans()
    assert span.attributes[GEN_AI_TOOL_CALL_ARGUMENTS] == "{}"


def test_per_request_config_can_enable_capture(tracing, monkeypatch):
    monkeypatch.delenv(OTEL_CAPTURE_ENV, raising=False)
    exporter, provider = tracing
    _run_callback(
        provider,
        _tool_context(TelemetryConfig(capture_message_content=ContentCapturingMode.SPAN_ONLY)),
        {"namespace": "default"},
        {"pods": 2},
    )

    (span,) = exporter.get_finished_spans()
    assert json.loads(span.attributes[GEN_AI_TOOL_CALL_ARGUMENTS]) == {"namespace": "default"}


def test_noop_without_recording_span():
    callback = make_tool_span_attributes_callback()
    # No span is active on this call path: the current span is the non-recording
    # INVALID_SPAN and the callback must not raise.
    assert trace.get_current_span() is trace.INVALID_SPAN
    assert callback(tool=SimpleNamespace(name="x"), args={}, tool_context=_tool_context(), tool_response={}) is None
