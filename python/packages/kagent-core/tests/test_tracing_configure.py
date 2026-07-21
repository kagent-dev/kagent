from types import SimpleNamespace

import pytest
from opentelemetry.propagate import get_global_textmap
from opentelemetry.trace import get_current_span

from kagent.core.tracing import _utils


def test_configure_tracing_logging_enabled_uses_event_logger_provider(monkeypatch):
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "true")
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "false")

    instrument_calls = {}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["init_kwargs"] = kwargs

        def instrument(self, **kwargs):
            instrument_calls["instrument_kwargs"] = kwargs

    class FakeLogRecordProcessor:
        def shutdown(self) -> None:
            instrument_calls["log_processor_shutdown"] = True

    def fake_event_logger_provider(logger_provider):
        event_logger_provider = {"logger_provider": logger_provider}
        instrument_calls["event_logger_provider"] = event_logger_provider
        return event_logger_provider

    def fake_instrument_anthropic(event_logger_provider=None):
        instrument_calls["anthropic_event_logger_provider"] = event_logger_provider

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "EventLoggerProvider", fake_event_logger_provider)
    monkeypatch.setattr(_utils, "_create_log_exporter", lambda *args, **kwargs: object())
    monkeypatch.setattr(_utils, "BatchLogRecordProcessor", lambda *args, **kwargs: FakeLogRecordProcessor())
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda: None)
    monkeypatch.setattr(
        _utils,
        "_logs",
        SimpleNamespace(set_logger_provider=lambda provider: instrument_calls.setdefault("logger_provider", provider)),
    )

    _utils.configure(name="test", namespace="test")

    assert instrument_calls["init_kwargs"] == {"use_legacy_attributes": False}
    assert "event_logger_provider" in instrument_calls["instrument_kwargs"]
    assert instrument_calls["anthropic_event_logger_provider"] == instrument_calls["event_logger_provider"]


def test_configure_tracing_logging_disabled_uses_legacy_instrumentation(monkeypatch):
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "false")
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "false")

    instrument_calls = {}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["init_kwargs"] = kwargs

        def instrument(self, **kwargs):
            instrument_calls["instrument_kwargs"] = kwargs

    def fake_instrument_anthropic(event_logger_provider=None):
        instrument_calls["anthropic_event_logger_provider"] = event_logger_provider

    def fake_instrument_google():
        instrument_calls["google_instrumented"] = True

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", fake_instrument_google)

    _utils.configure(name="test", namespace="test")

    assert instrument_calls["init_kwargs"] == {}
    assert instrument_calls["instrument_kwargs"] == {}
    assert instrument_calls["anthropic_event_logger_provider"] is None
    assert instrument_calls["google_instrumented"] is True


def _stub_tracing_side_effects(monkeypatch, instrument_calls):
    """Neutralise the OTLP/exporter side effects of configure() so tests can
    focus on which auto-instrumentors are (de)activated."""

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            pass

        def instrument(self, **kwargs):
            pass

    class FakeHTTPXInstrumentor:
        def instrument(self, **kwargs):
            instrument_calls["httpx_instrument_kwargs"] = kwargs

    class FakeFastAPIInstrumentor:
        def instrument_app(self, app, **kwargs):
            instrument_calls["fastapi_instrument_kwargs"] = kwargs

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "HTTPXClientInstrumentor", FakeHTTPXInstrumentor)
    monkeypatch.setattr(_utils, "FastAPIInstrumentor", FakeFastAPIInstrumentor)
    monkeypatch.setattr(_utils, "_instrument_anthropic", lambda *a, **k: None)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda: None)
    monkeypatch.setattr(_utils, "_create_span_exporter", lambda *a, **k: object())
    monkeypatch.setattr(_utils, "BatchSpanProcessor", lambda *a, **k: object())
    monkeypatch.setattr(_utils, "KagentAttributesSpanProcessor", lambda *a, **k: object())

    class FakeTracerProvider:
        def __init__(self, *a, **k):
            pass

        def add_span_processor(self, processor):
            pass

    # Stub the provider so configure() neither mutates global state nor registers
    # a real atexit shutdown hook (the real SDK provider would).
    monkeypatch.setattr(_utils, "TracerProvider", FakeTracerProvider)
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: object())
    monkeypatch.setattr(_utils.trace, "set_tracer_provider", lambda provider: None)


def test_configure_httpx_client_instrumentation_enabled_by_default(monkeypatch):
    """httpx client-transport spans are on by default (upstream parity); they
    carry outbound-call timing and on-wire trace context."""
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "true")
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "false")
    monkeypatch.delenv("OTEL_INSTRUMENTATION_HTTPX_CLIENT_ENABLED", raising=False)

    instrument_calls = {}
    _stub_tracing_side_effects(monkeypatch, instrument_calls)

    _utils.configure(name="test", namespace="test")

    assert "httpx_instrument_kwargs" in instrument_calls
    assert "excluded_urls" in instrument_calls["httpx_instrument_kwargs"]


def test_configure_httpx_client_instrumentation_opt_out_via_env(monkeypatch):
    """Operators can opt out of the raw transport spans for leaner traces."""
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "true")
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "false")
    monkeypatch.setenv("OTEL_INSTRUMENTATION_HTTPX_CLIENT_ENABLED", "false")

    instrument_calls = {}
    _stub_tracing_side_effects(monkeypatch, instrument_calls)

    _utils.configure(name="test", namespace="test")

    assert "httpx_instrument_kwargs" not in instrument_calls


def test_configure_fastapi_keeps_standard_asgi_spans(monkeypatch):
    """FastAPI is instrumented with the standard ASGI spans (upstream parity);
    only the agent-card health-check endpoint is excluded via excluded_urls."""
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "true")
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "false")

    instrument_calls = {}
    _stub_tracing_side_effects(monkeypatch, instrument_calls)

    _utils.configure(name="test", namespace="test", fastapi_app=object())

    kwargs = instrument_calls["fastapi_instrument_kwargs"]
    assert "exclude_spans" not in kwargs
    assert "excluded_urls" in kwargs


def test_otel_sdk_default_propagator_includes_w3c_tracecontext():
    """The OTEL SDK must propagate W3C TraceContext by default.

    kagent relies on this to extract incoming traceparent headers without any
    explicit set_global_textmap call.  If an OTEL SDK upgrade removes this
    default, this test will fail and explicit configuration will be needed.
    """
    trace_id = 0x4BF92F3577B34DA6A3CE929D0E0E4736
    span_id = 0x00F067AA0BA902B7
    carrier = {"traceparent": f"00-{trace_id:032x}-{span_id:016x}-01"}

    ctx = get_global_textmap().extract(carrier)
    assert get_current_span(ctx).get_span_context().trace_id == trace_id


@pytest.mark.parametrize(
    ("signal", "env", "expected"),
    [
        ("TRACES", {}, 10.0),
        ("TRACES", {"OTEL_EXPORTER_OTLP_TIMEOUT": "500"}, 0.5),
        ("TRACES", {"OTEL_EXPORTER_OTLP_TRACES_TIMEOUT": "250"}, 0.25),
        (
            "LOGS",
            {
                "OTEL_EXPORTER_OTLP_TIMEOUT": "500",
                "OTEL_EXPORTER_OTLP_LOGS_TIMEOUT": "750",
            },
            0.75,
        ),
    ],
)
def test_resolve_otlp_timeout_seconds_uses_milliseconds(monkeypatch, signal, env, expected):
    for key in ("OTEL_EXPORTER_OTLP_TIMEOUT", "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT", "OTEL_EXPORTER_OTLP_LOGS_TIMEOUT"):
        monkeypatch.delenv(key, raising=False)
    for key, value in env.items():
        monkeypatch.setenv(key, value)

    assert _utils._resolve_otlp_timeout_seconds(signal) == expected
