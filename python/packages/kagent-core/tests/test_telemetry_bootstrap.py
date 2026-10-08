from types import SimpleNamespace

import pytest
from fastapi import FastAPI
from fastapi.responses import StreamingResponse
from fastapi.testclient import TestClient
from opentelemetry.propagate import get_global_textmap, set_global_textmap
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import HistogramDataPoint, InMemoryMetricReader
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import get_current_span

from kagent.core.telemetry import _boot, _defaults
from kagent.core.telemetry._conv import KagentRuntimeValues
from kagent.core.telemetry._profiles import PROFILES, profile_for
from kagent.core.telemetry._span_processor import KagentAttributesSpanProcessor

_RESOURCE_ENV = ("OTEL_SERVICE_NAME", "OTEL_RESOURCE_ATTRIBUTES", "KAGENT_NAME", "KAGENT_NAMESPACE")


@pytest.fixture(autouse=True)
def fresh_bootstrap(monkeypatch):
    monkeypatch.setattr(_boot, "_current", None)
    for name in _RESOURCE_ENV + ("OTEL_SDK_DISABLED", "OTEL_PYTHON_DISABLED_INSTRUMENTATIONS"):
        monkeypatch.delenv(name, raising=False)
    previous = get_global_textmap()
    yield
    set_global_textmap(previous)


@pytest.fixture
def configurator(monkeypatch):
    calls = SimpleNamespace(configure=[], instrument=[])
    monkeypatch.setattr(
        _boot.OpenTelemetryConfigurator, "configure", lambda self, **kwargs: calls.configure.append(kwargs)
    )
    monkeypatch.setattr(_boot, "_instrument", lambda names, options: calls.instrument.append((names, options)))
    return calls


def test_bootstrap_declares_runtime_and_identity(monkeypatch, configurator):
    monkeypatch.setenv("KAGENT_NAME", "researcher-kagent")
    monkeypatch.setenv("KAGENT_NAMESPACE", "team-a")

    providers = _boot.bootstrap(KagentRuntimeValues.LANGGRAPH, fallback_name="ignored")

    (kwargs,) = configurator.configure
    assert kwargs["resource_attributes"] == {
        "service.name": "researcher-kagent",
        "service.namespace": "team-a",
        "kagent.runtime": "langgraph",
    }
    assert [type(processor) for processor in kwargs["span_processors"]] == [KagentAttributesSpanProcessor]
    assert providers.identity.attributes() == {
        "gen_ai.agent.name": "researcher-kagent",
        "gen_ai.agent.id": "team-a/researcher-kagent",
    }
    assert configurator.instrument == [(("httpx", "httpx2", "langchain"), {})]


def test_environment_identity_wins_over_defaults(monkeypatch, configurator):
    monkeypatch.setenv("OTEL_SERVICE_NAME", "compiled-name")
    monkeypatch.setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=compiled-ns")

    _boot.bootstrap(KagentRuntimeValues.ADK_PYTHON, fallback_name="card-name")

    (kwargs,) = configurator.configure
    assert kwargs["resource_attributes"] == {"kagent.runtime": "adk-python"}


def test_exporter_timeouts_are_read_as_milliseconds(monkeypatch, configurator):
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "500")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_LOGS_TIMEOUT", "750")

    _boot.bootstrap(KagentRuntimeValues.BYO)

    timeouts = {
        exporter.__module__.split(".")[4] + "." + exporter.__name__: args["timeout"]
        for exporter, args in configurator.configure[0]["exporter_args_map"].items()
    }
    assert timeouts == {
        "grpc.OTLPSpanExporter": 0.5,
        "http.OTLPSpanExporter": 0.5,
        "grpc.OTLPMetricExporter": 0.5,
        "http.OTLPMetricExporter": 0.5,
        "grpc.OTLPLogExporter": 0.75,
        "http.OTLPLogExporter": 0.75,
    }


def test_disabled_sdk_installs_nothing(monkeypatch, configurator):
    monkeypatch.setenv("OTEL_SDK_DISABLED", "true")

    providers = _boot.bootstrap(KagentRuntimeValues.CREWAI)

    assert configurator.configure == []
    assert configurator.instrument == []
    assert providers.profile == PROFILES[KagentRuntimeValues.CREWAI]


def test_no_exporting_signal_installs_no_instrumentation(monkeypatch, configurator):
    for signal in ("TRACES", "METRICS", "LOGS"):
        monkeypatch.setenv(f"OTEL_{signal}_EXPORTER", "none")

    _boot.bootstrap(KagentRuntimeValues.OPENAI_AGENTS)

    assert len(configurator.configure) == 1
    assert configurator.instrument == []


def test_configuration_errors_never_stop_startup(monkeypatch):
    def boom(self, **kwargs):
        raise RuntimeError("bad exporter")

    installed = []
    monkeypatch.setattr(_boot.OpenTelemetryConfigurator, "configure", boom)
    monkeypatch.setattr(_boot, "_instrument", lambda names, options: installed.append(names))

    providers = _boot.bootstrap(KagentRuntimeValues.ADK_PYTHON, fallback_name="agent")

    assert providers.identity.agent_name == "agent"
    assert installed == []


def test_bootstrap_runs_once(configurator):
    first = _boot.bootstrap(KagentRuntimeValues.BYO)

    assert _boot.bootstrap(KagentRuntimeValues.LANGGRAPH) is first
    assert len(configurator.configure) == 1


def test_disabled_instrumentations_are_skipped(monkeypatch):
    instrumented = []

    class Instrumentor:
        def __init__(self, name):
            self.name = name

        def instrument(self, **kwargs):
            instrumented.append((self.name, kwargs))

    def entry_points(group, name):
        return [SimpleNamespace(load=lambda: lambda: Instrumentor(name))]

    monkeypatch.setattr(_boot, "entry_points", entry_points)
    monkeypatch.setenv("OTEL_PYTHON_DISABLED_INSTRUMENTATIONS", "httpx, openai")

    _boot._instrument(("httpx", "openai_agents", "openai"), {"openai_agents": {"disable_openai_trace_export": True}})

    assert instrumented == [("openai_agents", {"disable_openai_trace_export": True})]


def test_every_python_runtime_has_a_profile():
    python = {
        KagentRuntimeValues.ADK_PYTHON,
        KagentRuntimeValues.LANGGRAPH,
        KagentRuntimeValues.OPENAI_AGENTS,
        KagentRuntimeValues.CREWAI,
        KagentRuntimeValues.BYO,
    }
    assert set(PROFILES) == python
    for runtime in set(KagentRuntimeValues) - python:
        with pytest.raises(ValueError):
            profile_for(runtime)


@pytest.mark.parametrize(
    ("runtime", "instrumentors", "emits_invoke_agent"),
    [
        (KagentRuntimeValues.ADK_PYTHON, (), True),
        (KagentRuntimeValues.LANGGRAPH, ("langchain",), False),
        (KagentRuntimeValues.OPENAI_AGENTS, ("openai_agents", "openai"), True),
        (KagentRuntimeValues.CREWAI, ("openai", "anthropic"), False),
        (KagentRuntimeValues.BYO, (), False),
    ],
)
def test_profile_matrix(runtime, instrumentors, emits_invoke_agent):
    profile = PROFILES[runtime]
    assert profile.instrumentors == instrumentors
    assert profile.emits_invoke_agent is emits_invoke_agent


def test_profile_instrumentations_are_installed():
    from opentelemetry.util._importlib_metadata import entry_points

    for profile in PROFILES.values():
        for name in profile.instrumentors + _boot.COMMON_INSTRUMENTORS:
            assert list(entry_points(group="opentelemetry_instrumentor", name=name)), name


def test_defaults_keep_explicit_values(monkeypatch):
    monkeypatch.setenv("OTEL_SEMCONV_STABILITY_OPT_IN", "gen_ai_latest_experimental")
    monkeypatch.delenv("OTEL_INSTRUMENTATION_A2A_SDK_ENABLED", raising=False)

    _defaults.apply()

    import os

    assert os.environ["OTEL_SEMCONV_STABILITY_OPT_IN"] == "gen_ai_latest_experimental"
    assert os.environ["OTEL_INSTRUMENTATION_A2A_SDK_ENABLED"] == "false"


def test_propagates_trace_context_without_baggage(monkeypatch, configurator):
    monkeypatch.setenv("OTEL_PROPAGATORS", "")

    _boot.bootstrap(KagentRuntimeValues.BYO)

    assert sorted(get_global_textmap().fields) == ["traceparent", "tracestate"]
    trace_id = 0x4BF92F3577B34DA6A3CE929D0E0E4736
    carrier = {"traceparent": f"00-{trace_id:032x}-00f067aa0ba902b7-01"}
    assert get_current_span(get_global_textmap().extract(carrier)).get_span_context().trace_id == trace_id


@pytest.mark.parametrize(
    ("env", "expected"),
    [
        ({}, True),
        ({"OTEL_TRACES_EXPORTER": "none"}, False),
        ({"OTEL_TRACES_EXPORTER": "console,otlp"}, True),
        ({"OTEL_TRACES_EXPORTER": "console"}, True),
        ({"OTEL_TRACES_EXPORTER": "otlp", "OTEL_SDK_DISABLED": "true"}, False),
    ],
)
def test_signal_enabled_follows_the_sdk(monkeypatch, env, expected):
    monkeypatch.delenv("OTEL_TRACES_EXPORTER", raising=False)
    for key, value in env.items():
        monkeypatch.setenv(key, value)

    assert _boot.signal_enabled("TRACES") is expected


def test_force_flush_flushes_every_provider_within_the_budget(monkeypatch):
    calls = []
    provider = SimpleNamespace(force_flush=lambda timeout: calls.append(timeout))
    monkeypatch.setattr(_boot._logs, "get_logger_provider", lambda: provider)
    monkeypatch.setattr(_boot.trace, "get_tracer_provider", lambda: provider)
    monkeypatch.setattr(_boot.metrics, "get_meter_provider", lambda: SimpleNamespace())

    _boot.force_flush()

    assert calls == [3000, 3000]


def test_force_flush_swallows_exporter_errors(monkeypatch):
    def boom(timeout):
        raise RuntimeError("collector down")

    monkeypatch.setattr(_boot.trace, "get_tracer_provider", lambda: SimpleNamespace(force_flush=boom))

    _boot.force_flush()


def _streaming_app():
    app = FastAPI()

    async def chunks():
        for chunk in ("a", "b", "c"):
            yield chunk

    @app.post("/")
    async def root():
        return StreamingResponse(chunks(), media_type="text/event-stream")

    @app.get("/health")
    async def health():
        return {"ok": True}

    return app


def test_inbound_http_follows_stable_semantic_conventions():
    exporter = InMemorySpanExporter()
    tracer_provider = TracerProvider()
    tracer_provider.add_span_processor(SimpleSpanProcessor(exporter))
    reader = InMemoryMetricReader()
    app = _streaming_app()
    _boot.instrument_app(app, tracer_provider=tracer_provider, meter_provider=MeterProvider(metric_readers=[reader]))

    with TestClient(app) as client:
        assert client.post("/").text == "abc"
        client.get("/health")

    spans = exporter.get_finished_spans()
    assert [span.name for span in spans] == ["POST /"]
    assert spans[0].attributes["http.request.method"] == "POST"

    metrics = {
        metric.name: metric
        for resource in reader.get_metrics_data().resource_metrics
        for scope in resource.scope_metrics
        for metric in scope.metrics
    }
    duration = metrics["http.server.request.duration"]
    assert duration.unit == "s"
    (point,) = duration.data.data_points
    assert isinstance(point, HistogramDataPoint)
    assert set(point.attributes) == {
        "http.request.method",
        "http.route",
        "http.response.status_code",
        "network.protocol.version",
        "url.scheme",
    }
    assert "http.server.duration" not in metrics
