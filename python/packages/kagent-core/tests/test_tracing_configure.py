import asyncio
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


def test_configure_tracing_only_uses_legacy_instrumentation(monkeypatch):
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "false")
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "true")

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


def test_configure_all_disabled_skips_instrumentation(monkeypatch):
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "false")
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "false")

    instrument_calls = {"openai_instrumented": False, "google_instrumented": False}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

        def instrument(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

    def fake_instrument_anthropic(event_logger_provider=None):
        instrument_calls["anthropic_called"] = True

    def fake_instrument_google():
        instrument_calls["google_instrumented"] = True

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", fake_instrument_google)

    _utils.configure(name="test", namespace="test")

    # With no signal enabled, telemetry must not touch the OpenAI SDK at all.
    assert instrument_calls["openai_instrumented"] is False
    assert instrument_calls["google_instrumented"] is False
    assert "anthropic_called" not in instrument_calls


@pytest.mark.parametrize("logging_enabled", [True, False])
def test_configure_instrument_openai_client_false_skips_openai(monkeypatch, logging_enabled):
    # A signal is enabled either way; only the OpenAI client instrumentor must be skipped.
    monkeypatch.setenv("OTEL_LOGGING_ENABLED", "true" if logging_enabled else "false")
    monkeypatch.setenv("OTEL_TRACING_ENABLED", "true")

    instrument_calls = {"openai_instrumented": False}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

        def instrument(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

    def fake_instrument_anthropic(event_logger_provider=None):
        instrument_calls["anthropic_called"] = True

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "EventLoggerProvider", lambda logger_provider: {"lp": logger_provider})
    monkeypatch.setattr(_utils, "_create_log_exporter", lambda *args, **kwargs: object())
    monkeypatch.setattr(
        _utils, "BatchLogRecordProcessor", lambda *args, **kwargs: SimpleNamespace(shutdown=lambda: None)
    )
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda: None)
    monkeypatch.setattr(_utils, "_logs", SimpleNamespace(set_logger_provider=lambda provider: None))

    _utils.configure(name="test", namespace="test", instrument_openai_client=False)

    # Higher-level instrumentors own OpenAI; anthropic still runs.
    assert instrument_calls["openai_instrumented"] is False
    assert instrument_calls["anthropic_called"] is True


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


def test_force_flush_calls_provider_force_flush(monkeypatch):
    calls = []
    provider = SimpleNamespace(force_flush=lambda timeout: calls.append(timeout))
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: provider)

    _utils.force_flush()

    assert calls == [3000]


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("500", 500),
        ("not-a-number", 3000),
        ("0", 3000),
        ("-100", 3000),
    ],
)
def test_force_flush_timeout_env_override(monkeypatch, raw, expected):
    calls = []
    provider = SimpleNamespace(force_flush=lambda timeout: calls.append(timeout))
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: provider)
    monkeypatch.setenv("KAGENT_TRACE_FLUSH_TIMEOUT_MS", raw)

    _utils.force_flush()

    assert calls == [expected]


def test_force_flush_noop_without_provider_support(monkeypatch):
    # The default (no-op) provider has no force_flush; must not raise.
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: SimpleNamespace())

    _utils.force_flush()


def test_force_flush_swallows_exporter_errors(monkeypatch):
    def boom(timeout):
        raise RuntimeError("collector down")

    provider = SimpleNamespace(force_flush=boom)
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: provider)

    _utils.force_flush()


def _flush_test_app():
    from fastapi import FastAPI

    app = FastAPI()

    @app.post("/")
    async def root():
        return {"ok": True}

    @app.get("/health")
    async def health():
        return {"status": "ok"}

    return app


def test_post_response_flush_skips_excluded_paths(monkeypatch):
    from fastapi.testclient import TestClient

    calls = []
    monkeypatch.setattr(_utils, "force_flush", lambda: calls.append(True))

    app = _flush_test_app()
    _utils._add_post_response_flush(app)
    client = TestClient(app)

    assert client.get("/health").status_code == 200
    assert calls == []

    assert client.post("/").status_code == 200
    assert calls == [True]


def test_post_response_flush_precedes_terminal_body(monkeypatch):
    from fastapi import FastAPI

    events = []
    app = FastAPI()

    async def instrumented_app(scope, receive, send):
        await send({"type": "http.response.start", "status": 200})
        await send({"type": "http.response.body", "body": b"ok"})
        events.append("server-span-ended")

    app.build_middleware_stack = lambda: instrumented_app
    monkeypatch.setattr(_utils, "force_flush", lambda: events.append("flushed"))
    _utils._add_post_response_flush(app)

    async def send(message):
        events.append(message["type"])

    asyncio.run(app.build_middleware_stack()({"type": "http", "path": "/"}, None, send))

    assert events == ["http.response.start", "server-span-ended", "flushed", "http.response.body"]


def test_post_response_flush_sends_terminal_body_when_app_raises(monkeypatch):
    # An exception raised after the app produced its terminal body must not
    # swallow that body: the middleware holds it back for the flush, so it is
    # responsible for forwarding it even on the error path. The exception
    # itself still propagates.
    from fastapi import FastAPI

    events = []
    app = FastAPI()

    async def instrumented_app(scope, receive, send):
        await send({"type": "http.response.start", "status": 200})
        await send({"type": "http.response.body", "body": b"ok"})
        raise RuntimeError("post-response instrumentation failure")

    app.build_middleware_stack = lambda: instrumented_app
    monkeypatch.setattr(_utils, "force_flush", lambda: events.append("flushed"))
    _utils._add_post_response_flush(app)

    async def send(message):
        events.append(message["type"])

    with pytest.raises(RuntimeError, match="post-response instrumentation failure"):
        asyncio.run(app.build_middleware_stack()({"type": "http", "path": "/"}, None, send))

    assert events == ["http.response.start", "flushed", "http.response.body"]


def test_post_response_flush_exports_server_span(monkeypatch):
    # The inbound server span ends inside the OTel middleware's send wrapper,
    # after the executor-level work is long done — only a flush wrapped
    # *outside* that middleware can export it. Uses a batch processor so
    # nothing is exported unless the flush actually runs.
    from fastapi.testclient import TestClient
    from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor
    from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(BatchSpanProcessor(exporter))
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: provider)

    app = _flush_test_app()
    FastAPIInstrumentor().instrument_app(app, tracer_provider=provider)
    _utils._add_post_response_flush(app)

    with TestClient(app) as client:
        assert client.post("/").status_code == 200

    names = [span.name for span in exporter.get_finished_spans()]
    assert any("POST" in name for name in names), f"server span not exported by flush, got {names}"
    provider.shutdown()
