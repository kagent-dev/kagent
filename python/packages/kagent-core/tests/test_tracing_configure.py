import asyncio
from types import SimpleNamespace

import pytest
from opentelemetry.propagate import get_global_textmap, set_global_textmap
from opentelemetry.trace import get_current_span

from kagent.core.tracing import _utils


@pytest.fixture(autouse=True)
def _metrics_off_unless_asked(monkeypatch):
    monkeypatch.setenv("OTEL_METRICS_EXPORTER", "none")


def test_configure_tracing_logging_enabled_uses_logger_provider(monkeypatch):
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "otlp")
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "none")

    instrument_calls = {}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["init_kwargs"] = kwargs

        def instrument(self, **kwargs):
            instrument_calls["instrument_kwargs"] = kwargs

    class FakeLogRecordProcessor:
        def shutdown(self) -> None:
            instrument_calls["log_processor_shutdown"] = True

    def fake_instrument_anthropic(logger_provider=None):
        instrument_calls["anthropic_logger_provider"] = logger_provider

    def fake_instrument_google(logger_provider=None):
        instrument_calls["google_logger_provider"] = logger_provider

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "_create_log_exporter", lambda *args, **kwargs: object())
    monkeypatch.setattr(_utils, "BatchLogRecordProcessor", lambda *args, **kwargs: FakeLogRecordProcessor())
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", fake_instrument_google)
    monkeypatch.setattr(
        _utils,
        "_logs",
        SimpleNamespace(set_logger_provider=lambda provider: instrument_calls.setdefault("logger_provider", provider)),
    )

    _utils.configure(name="test", namespace="test")

    assert instrument_calls["init_kwargs"] == {"use_legacy_attributes": False}
    assert instrument_calls["instrument_kwargs"] == {"logger_provider": instrument_calls["logger_provider"]}
    assert instrument_calls["anthropic_logger_provider"] == instrument_calls["logger_provider"]
    assert instrument_calls["google_logger_provider"] == instrument_calls["logger_provider"]


def test_configure_tracing_only_uses_legacy_instrumentation(monkeypatch):
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "none")
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "otlp")

    instrument_calls = {}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["init_kwargs"] = kwargs

        def instrument(self, **kwargs):
            instrument_calls["instrument_kwargs"] = kwargs

    def fake_instrument_anthropic(logger_provider=None):
        instrument_calls["anthropic_logger_provider"] = logger_provider

    def fake_instrument_google(logger_provider=None):
        instrument_calls["google_instrumented"] = True
        instrument_calls["google_logger_provider"] = logger_provider

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", fake_instrument_google)

    _utils.configure(name="test", namespace="test")

    assert instrument_calls["init_kwargs"] == {}
    assert instrument_calls["instrument_kwargs"] == {}
    assert instrument_calls["anthropic_logger_provider"] is None
    assert instrument_calls["google_instrumented"] is True
    assert instrument_calls["google_logger_provider"] is None


@pytest.mark.parametrize(
    ("service_name", "expected_name", "expected_namespace"),
    [("demo-kagent", "demo-kagent", "team"), (None, "test-agent", "team")],
)
def test_configure_resource_lets_the_environment_win(monkeypatch, service_name, expected_name, expected_namespace):
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "none")
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "otlp")
    monkeypatch.setenv(
        "OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=prod,service.version=1.4.2,service.namespace=team"
    )
    if service_name is None:
        monkeypatch.delenv("OTEL_SERVICE_NAME", raising=False)
    else:
        monkeypatch.setenv("OTEL_SERVICE_NAME", service_name)

    captured = {}

    class FakeTracerProvider:
        def __init__(self, resource):
            captured["resource"] = resource

        def add_span_processor(self, processor):
            pass

    monkeypatch.setattr(_utils, "TracerProvider", FakeTracerProvider)
    monkeypatch.setattr(_utils, "_create_span_exporter", lambda **kwargs: object())
    monkeypatch.setattr(_utils, "BatchSpanProcessor", lambda exporter: object())
    monkeypatch.setattr(_utils.trace, "set_tracer_provider", lambda provider: None)
    monkeypatch.setattr(_utils, "HTTPXClientInstrumentor", lambda: SimpleNamespace(instrument=lambda **kw: None))
    monkeypatch.setattr(_utils, "OpenAIInstrumentor", lambda **kwargs: SimpleNamespace(instrument=lambda **kw: None))
    monkeypatch.setattr(_utils, "_instrument_anthropic", lambda *a, **kw: None)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda *a, **kw: None)

    _utils.configure(name="test-agent", namespace="test-ns")

    attributes = captured["resource"].attributes
    assert attributes["service.name"] == expected_name
    assert attributes["service.namespace"] == expected_namespace
    assert attributes["deployment.environment.name"] == "prod"
    assert attributes["service.version"] == "1.4.2"
    assert attributes["telemetry.sdk.language"] == "python"


def test_configure_all_disabled_skips_instrumentation(monkeypatch):
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "none")
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "none")

    instrument_calls = {"openai_instrumented": False, "google_instrumented": False}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

        def instrument(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

    def fake_instrument_anthropic(logger_provider=None):
        instrument_calls["anthropic_called"] = True

    def fake_instrument_google(logger_provider=None):
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
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "otlp" if logging_enabled else "none")
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "otlp")

    instrument_calls = {"openai_instrumented": False}

    class FakeOpenAIInstrumentor:
        def __init__(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

        def instrument(self, **kwargs):
            instrument_calls["openai_instrumented"] = True

    def fake_instrument_anthropic(logger_provider=None):
        instrument_calls["anthropic_called"] = True

    monkeypatch.setattr(_utils, "OpenAIInstrumentor", FakeOpenAIInstrumentor)
    monkeypatch.setattr(_utils, "_create_log_exporter", lambda *args, **kwargs: object())
    monkeypatch.setattr(
        _utils, "BatchLogRecordProcessor", lambda *args, **kwargs: SimpleNamespace(shutdown=lambda: None)
    )
    monkeypatch.setattr(_utils, "_instrument_anthropic", fake_instrument_anthropic)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda logger_provider=None: None)
    monkeypatch.setattr(_utils, "_logs", SimpleNamespace(set_logger_provider=lambda provider: None))

    _utils.configure(name="test", namespace="test", instrument_openai_client=False)

    # Higher-level instrumentors own OpenAI; anthropic still runs.
    assert instrument_calls["openai_instrumented"] is False
    assert instrument_calls["anthropic_called"] is True


def test_configure_propagates_trace_context_without_baggage(monkeypatch):
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "none")
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "none")
    monkeypatch.setenv("OTEL_PROPAGATORS", "")
    previous = get_global_textmap()
    try:
        _utils.configure(name="test", namespace="test")
        assert sorted(get_global_textmap().fields) == ["traceparent", "tracestate"]
        _assert_extracts_trace_context()
    finally:
        set_global_textmap(previous)


@pytest.mark.parametrize(
    ("env", "expected"),
    [
        ({}, True),
        ({"OTEL_TRACES_EXPORTER": "none"}, False),
        ({"OTEL_TRACES_EXPORTER": "console,otlp"}, True),
        ({"OTEL_TRACES_EXPORTER": "otlp", "OTEL_SDK_DISABLED": "true"}, False),
    ],
)
def test_signal_enabled_follows_the_sdk(monkeypatch, env, expected):
    for key in ("OTEL_TRACES_EXPORTER", "OTEL_SDK_DISABLED"):
        monkeypatch.delenv(key, raising=False)
    for key, value in env.items():
        monkeypatch.setenv(key, value)

    assert _utils.signal_enabled("TRACES") is expected


def _assert_extracts_trace_context():
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


@pytest.mark.parametrize(("traces", "expect_installed"), [("otlp", True), ("none", False)])
def test_configure_installs_post_response_flush_with_traces(monkeypatch, traces, expect_installed):
    from fastapi import FastAPI

    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "none")
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", traces)

    installed = []
    monkeypatch.setattr(_utils, "_add_post_response_flush", lambda app: installed.append(app))
    monkeypatch.setattr(
        _utils, "FastAPIInstrumentor", lambda: SimpleNamespace(instrument_app=lambda app, excluded_urls: None)
    )
    monkeypatch.setattr(_utils, "OpenAIInstrumentor", lambda **kwargs: SimpleNamespace(instrument=lambda **kw: None))
    monkeypatch.setattr(_utils, "_instrument_anthropic", lambda *a, **kw: None)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda *a, **kw: None)

    app = FastAPI()
    _utils.configure(name="test", namespace="test", fastapi_app=app)

    assert (installed == [app]) is expect_installed


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


def test_configure_exports_metrics_when_enabled(monkeypatch):
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "none")
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "none")
    monkeypatch.setenv("OTEL_METRICS_EXPORTER", "otlp")
    installed = {}
    monkeypatch.setattr(_utils, "_create_metric_exporter", lambda **kwargs: installed.setdefault("exporter", object()))
    monkeypatch.setattr(_utils, "PeriodicExportingMetricReader", lambda exporter: ("reader", exporter))
    monkeypatch.setattr(
        _utils, "MeterProvider", lambda resource, metric_readers: installed.setdefault("readers", metric_readers)
    )
    monkeypatch.setattr(
        _utils.metrics, "set_meter_provider", lambda provider: installed.setdefault("provider", provider)
    )
    flushes = []
    monkeypatch.setattr(_utils, "_add_post_response_flush", lambda app: flushes.append(app))
    monkeypatch.setattr(_utils, "_instrument_anthropic", lambda *a, **kw: None)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda *a, **kw: None)
    from fastapi import FastAPI

    app = FastAPI()
    _utils.configure(name="test", namespace="test", fastapi_app=app)

    assert installed["readers"] == [("reader", installed["exporter"])]
    assert installed["provider"] is installed["readers"]
    assert flushes == [app]


def test_force_flush_flushes_the_meter_provider(monkeypatch):
    calls = []
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: SimpleNamespace())
    monkeypatch.setattr(
        _utils.metrics, "get_meter_provider", lambda: SimpleNamespace(force_flush=lambda timeout: calls.append(timeout))
    )

    _utils.force_flush()

    assert calls == [3000]


def test_force_flush_flushes_logs_traces_and_metrics_in_order(monkeypatch):
    calls = []
    monkeypatch.setattr(
        _utils._logs, "get_logger_provider", lambda: SimpleNamespace(force_flush=lambda t: calls.append(("logs", t)))
    )
    monkeypatch.setattr(
        _utils.trace, "get_tracer_provider", lambda: SimpleNamespace(force_flush=lambda t: calls.append(("traces", t)))
    )
    monkeypatch.setattr(
        _utils.metrics,
        "get_meter_provider",
        lambda: SimpleNamespace(force_flush=lambda t: calls.append(("metrics", t))),
    )

    _utils.force_flush()

    assert calls == [("logs", 3000), ("traces", 3000), ("metrics", 3000)]


def test_force_flush_continues_when_log_flush_fails(monkeypatch):
    calls = []

    def boom(timeout):
        raise RuntimeError("collector down")

    monkeypatch.setattr(_utils._logs, "get_logger_provider", lambda: SimpleNamespace(force_flush=boom))
    monkeypatch.setattr(
        _utils.trace, "get_tracer_provider", lambda: SimpleNamespace(force_flush=lambda t: calls.append(t))
    )
    monkeypatch.setattr(_utils.metrics, "get_meter_provider", lambda: SimpleNamespace())

    _utils.force_flush()

    assert calls == [3000]


def test_force_flush_exports_buffered_logs(monkeypatch):
    from opentelemetry.sdk._logs import LoggerProvider
    from opentelemetry.sdk._logs.export import BatchLogRecordProcessor, InMemoryLogRecordExporter

    exporter = InMemoryLogRecordExporter()
    provider = LoggerProvider()
    provider.add_log_record_processor(BatchLogRecordProcessor(exporter))
    monkeypatch.setattr(_utils._logs, "get_logger_provider", lambda: provider)
    monkeypatch.setattr(_utils.trace, "get_tracer_provider", lambda: SimpleNamespace())
    monkeypatch.setattr(_utils.metrics, "get_meter_provider", lambda: SimpleNamespace())

    provider.get_logger("test").emit(body="buffered log")
    assert exporter.get_finished_logs() == ()

    _utils.force_flush()

    assert len(exporter.get_finished_logs()) == 1
    provider.shutdown()


def test_configure_installs_post_response_flush_with_logs_only(monkeypatch):
    from fastapi import FastAPI

    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "none")
    monkeypatch.setenv("OTEL_METRICS_EXPORTER", "none")
    monkeypatch.setenv("OTEL_LOGS_EXPORTER", "otlp")

    installed = []
    monkeypatch.setattr(_utils, "_add_post_response_flush", lambda app: installed.append(app))
    monkeypatch.setattr(_utils._logs, "set_logger_provider", lambda provider: None)
    monkeypatch.setattr(_utils, "OpenAIInstrumentor", lambda **kwargs: SimpleNamespace(instrument=lambda **kw: None))
    monkeypatch.setattr(_utils, "_instrument_anthropic", lambda *a, **kw: None)
    monkeypatch.setattr(_utils, "_instrument_google_generativeai", lambda *a, **kw: None)

    app = FastAPI()
    _utils.configure(name="test", namespace="test", fastapi_app=app)

    assert installed == [app]
