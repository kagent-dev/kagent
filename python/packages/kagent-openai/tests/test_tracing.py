"""Tracing wiring for the OpenAI Agents SDK runtime."""

import agents.tracing.setup as agents_tracing_setup
import pytest
from agents.tracing import get_trace_provider
from agents.tracing.processors import BackendSpanExporter
from agents.tracing.traces import NoOpTrace
from opentelemetry.instrumentation.openai_agents import OpenAIAgentsInstrumentor
from opentelemetry.instrumentation.openai_agents._hooks import OpenTelemetryTracingProcessor

from kagent.openai._a2a import _configure_openai_agents_tracing


def _reset():
    instrumentor = OpenAIAgentsInstrumentor()
    if instrumentor.is_instrumented_by_opentelemetry:
        instrumentor.uninstrument()
    # Cleared so the next get_trace_provider() rebuilds the SDK default provider,
    # which is the state a fresh agent process starts in.
    agents_tracing_setup.GLOBAL_TRACE_PROVIDER = None


@pytest.fixture(autouse=True)
def reset_agents_tracing(monkeypatch):
    monkeypatch.delenv("KAGENT_OPENAI_AGENTS_NATIVE_TRACING", raising=False)
    monkeypatch.delenv("OPENAI_AGENTS_DISABLE_TRACING", raising=False)
    _reset()
    yield
    _reset()


def _processors():
    return list(get_trace_provider()._multi_processor._processors)


def _exports_to_openai(processors):
    return any(isinstance(getattr(p, "_exporter", None), BackendSpanExporter) for p in processors)


def test_default_provider_exports_to_openai():
    """Guards the premise: the SDK ships the api.openai.com exporter by default."""
    assert _exports_to_openai(_processors())


def test_native_exporter_dropped_by_default():
    _configure_openai_agents_tracing()

    processors = _processors()
    assert not _exports_to_openai(processors)
    assert [type(p) for p in processors] == [OpenTelemetryTracingProcessor]


def test_repeat_configuration_keeps_opentelemetry_processor():
    """BaseInstrumentor.instrument() no-ops once instrumented.

    Anything that cleared processors outside _instrument would wipe the
    OpenTelemetry processor on a second call with nothing to reinstall it.
    """
    _configure_openai_agents_tracing()
    _configure_openai_agents_tracing()

    processors = _processors()
    assert not _exports_to_openai(processors)
    assert [type(p) for p in processors] == [OpenTelemetryTracingProcessor]


def test_spans_still_reach_opentelemetry():
    """Dropping the native exporter must not disable SDK tracing altogether."""
    _configure_openai_agents_tracing()

    assert not isinstance(get_trace_provider().create_trace("test"), NoOpTrace)


def test_native_exporter_kept_when_opted_in(monkeypatch):
    monkeypatch.setenv("KAGENT_OPENAI_AGENTS_NATIVE_TRACING", "true")

    _configure_openai_agents_tracing()

    processors = _processors()
    assert _exports_to_openai(processors)
    assert any(isinstance(p, OpenTelemetryTracingProcessor) for p in processors)


def test_build_drops_native_exporter_even_if_configure_tracing_fails(monkeypatch):
    """A broken OTLP setup must not leave the SDK shipping traces to OpenAI."""
    from agents import Agent
    from kagent.core import KAgentConfig

    from kagent.openai import _a2a

    monkeypatch.setenv("OTEL_TRACING_ENABLED", "true")

    def boom(*args, **kwargs):
        raise RuntimeError("no collector")

    monkeypatch.setattr(_a2a, "configure_tracing", boom)

    agent_card = {
        "name": "test",
        "description": "test agent",
        "version": "0.0.1",
        "supportedInterfaces": [{"url": "http://localhost:8080", "protocolBinding": "JSONRPC"}],
        "capabilities": {"streaming": True},
        "defaultInputModes": ["text/plain"],
        "defaultOutputModes": ["text/plain"],
        "skills": [],
    }
    app = _a2a.KAgentApp(
        agent=Agent(name="test"),
        agent_card=agent_card,
        config=KAgentConfig(
            api_url="http://localhost",
            gateway_url="http://localhost",
            name="test",
            namespace="test",
        ),
    )
    app.build()

    assert not _exports_to_openai(_processors())


def test_warns_when_sdk_tracing_disabled_by_env(monkeypatch, caplog):
    monkeypatch.setenv("OPENAI_AGENTS_DISABLE_TRACING", "1")

    with caplog.at_level("WARNING"):
        _configure_openai_agents_tracing()

    assert "OPENAI_AGENTS_DISABLE_TRACING" in caplog.text
