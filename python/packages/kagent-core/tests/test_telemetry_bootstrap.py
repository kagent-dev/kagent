import os
from types import SimpleNamespace

import pytest
from fastapi import FastAPI
from fastapi.responses import StreamingResponse
from fastapi.testclient import TestClient
from opentelemetry.instrumentation._semconv import _OpenTelemetrySemanticConventionStability
from opentelemetry.propagate import get_global_textmap, set_global_textmap
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import HistogramDataPoint, InMemoryMetricReader
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import get_current_span

from kagent.core._runtime_config import OTLPConfig, RuntimeConfig, TelemetryConfig
from kagent.core.telemetry import _boot, _defaults
from kagent.core.telemetry._conv import KagentRuntimeValues
from kagent.core.telemetry._profiles import PROFILES, profile_for
from kagent.core.telemetry._span_processor import KagentAttributesSpanProcessor

_RESOURCE_ENV = ("OTEL_SERVICE_NAME", "OTEL_RESOURCE_ATTRIBUTES")


@pytest.fixture(autouse=True)
def fresh_bootstrap(monkeypatch):
    monkeypatch.setattr(_boot, "_current", None)
    # OTEL caches env opt-ins on first instrumentation. Reset that cache too,
    # so these startup tests model a fresh process regardless of test order.
    monkeypatch.setattr(_OpenTelemetrySemanticConventionStability, "_initialized", False)
    monkeypatch.setattr(_OpenTelemetrySemanticConventionStability, "_OTEL_SEMCONV_STABILITY_SIGNAL_MAPPING", {})
    previous_environment = dict(os.environ)
    for name in tuple(os.environ):
        if not name.startswith("OTEL_") and name not in _RESOURCE_ENV:
            continue
        monkeypatch.delenv(name, raising=False)
    previous = get_global_textmap()
    yield
    set_global_textmap(previous)
    os.environ.clear()
    os.environ.update(previous_environment)


@pytest.fixture
def configurator(monkeypatch):
    calls = SimpleNamespace(configure=[], instrument=[])
    monkeypatch.setattr(
        _boot, "_install_providers", lambda identity, config: calls.configure.append((identity, config))
    )
    monkeypatch.setattr(_boot, "_instrument", lambda names, options: calls.instrument.append((names, options)))
    return calls


def exporting_config(**kwargs):
    return RuntimeConfig(
        telemetry=TelemetryConfig(traces=OTLPConfig(endpoint="http://collector:4317", protocol="grpc")),
        **kwargs,
    )


def test_bootstrap_uses_config_identity_and_environment_telemetry(monkeypatch, configurator):
    monkeypatch.setenv("OTEL_SERVICE_NAME", "ignored")
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "otlp")
    config = exporting_config(name="researcher-kagent", namespace="team-a")
    providers = _boot.bootstrap(KagentRuntimeValues.LANGGRAPH, fallback_name="ignored", config=config)
    identity, exporters = configurator.configure[0]
    assert _boot._resource_attributes(identity, exporters) == {
        "service.name": "ignored",
        "service.namespace": "team-a",
        "kagent.runtime": "langgraph",
    }
    assert providers.identity.attributes() == {
        "gen_ai.agent.name": "researcher-kagent",
        "gen_ai.agent.id": "team-a/researcher-kagent",
    }
    assert configurator.instrument == [(("httpx", "httpx2", "langchain"), {})]
    assert _boot.signal_enabled("TRACES")
    assert not _boot.signal_enabled("METRICS")


def test_absent_exporters_disable_instrumentation(monkeypatch, configurator):
    providers = _boot.bootstrap(KagentRuntimeValues.CREWAI)
    assert configurator.instrument == []
    assert not _boot.signal_enabled("TRACES")
    assert providers.profile == PROFILES[KagentRuntimeValues.CREWAI]


def test_invalid_exporter_logs_and_continues(monkeypatch, caplog):
    monkeypatch.setenv("OTEL_TRACES_EXPORTER", "jaeger")
    instrumented = []
    monkeypatch.setattr(_boot, "_instrument", lambda names, options: instrumented.extend(names))
    providers = _boot.bootstrap(KagentRuntimeValues.ADK_PYTHON, config=exporting_config(name="agent"))
    assert providers.identity.agent_name == "agent"
    assert _boot.current() is providers
    assert instrumented
    assert "Failed to configure OpenTelemetry providers" in caplog.text
    assert "jaeger" in caplog.text


def test_bootstrap_runs_once(configurator):
    first = _boot.bootstrap(KagentRuntimeValues.BYO)
    assert _boot.bootstrap(KagentRuntimeValues.LANGGRAPH) is first
    assert len(configurator.configure) == 1


def test_instrumentation_selection_honors_environment(monkeypatch):
    instrumented = []

    class Instrumentor:
        def __init__(self, name):
            self.name = name

        def instrument(self, **kwargs):
            instrumented.append((self.name, kwargs))

    monkeypatch.setattr(
        _boot, "entry_points", lambda group, name: [SimpleNamespace(load=lambda: lambda: Instrumentor(name))]
    )
    monkeypatch.setenv("OTEL_PYTHON_DISABLED_INSTRUMENTATIONS", "httpx, openai")
    _boot._instrument(("httpx", "openai_agents"), {"openai_agents": {"disable_openai_trace_export": True}})
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


def test_instrumentation_adapter_preserves_overrides(monkeypatch):
    monkeypatch.setenv("OTEL_SEMCONV_STABILITY_OPT_IN", "gen_ai_latest_experimental")
    monkeypatch.delenv("OTEL_INSTRUMENTATION_A2A_SDK_ENABLED", raising=False)

    _defaults.apply(TelemetryConfig())

    import os

    assert os.environ["OTEL_SEMCONV_STABILITY_OPT_IN"] == "gen_ai_latest_experimental"
    assert os.environ["OTEL_INSTRUMENTATION_A2A_SDK_ENABLED"] == "false"


def test_propagates_trace_context_without_baggage(monkeypatch, configurator):
    monkeypatch.delenv("OTEL_PROPAGATORS", raising=False)

    _boot.bootstrap(KagentRuntimeValues.BYO)

    assert sorted(get_global_textmap().fields) == ["traceparent", "tracestate"]
    trace_id = 0x4BF92F3577B34DA6A3CE929D0E0E4736
    carrier = {"traceparent": f"00-{trace_id:032x}-00f067aa0ba902b7-01"}
    assert get_current_span(get_global_textmap().extract(carrier)).get_span_context().trace_id == trace_id


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


def test_inbound_http_follows_stable_semantic_conventions(configurator):
    _boot.bootstrap(KagentRuntimeValues.BYO, config=exporting_config())
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


@pytest.mark.parametrize("prefix", ["OTEL_EXPORTER_OTLP_", "OTEL_EXPORTER_OTLP_TRACES_"])
def test_environment_exporter_overrides_file(monkeypatch, configurator, prefix):
    monkeypatch.setenv(prefix + "ENDPOINT", "http://override:4318/v1/traces")
    monkeypatch.setenv(prefix + "PROTOCOL", "http/protobuf")
    monkeypatch.setenv(prefix + "TIMEOUT", "1200")
    _boot.bootstrap(KagentRuntimeValues.BYO, config=exporting_config())
    assert (
        os.environ.get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"))
        == "http://override:4318/v1/traces"
    )
    assert _boot._resolve_otlp_timeout_seconds("TRACES") == 1.2


def test_environment_can_enable_and_disable_export(monkeypatch, configurator):
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://collector:4317")
    _boot.bootstrap(KagentRuntimeValues.BYO)
    assert _boot.signal_enabled("TRACES")
    assert not _boot.signal_enabled("METRICS")
    monkeypatch.setenv("OTEL_SDK_DISABLED", "true")
    assert not _boot.signal_enabled("TRACES")


@pytest.mark.parametrize("prefix", ["OTEL_EXPORTER_OTLP_", "OTEL_EXPORTER_OTLP_TRACES_"])
@pytest.mark.parametrize("option,value", [("ENDPOINT", "http://override:4317"), ("PROTOCOL", "http/protobuf")])
def test_partial_connection_override_replaces_file_pair(monkeypatch, prefix, option, value):
    monkeypatch.setenv(prefix + option, value)
    _defaults.apply(exporting_config().telemetry)
    assert os.environ[prefix + option] == value
    missing = "PROTOCOL" if option == "ENDPOINT" else "ENDPOINT"
    assert "OTEL_EXPORTER_OTLP_TRACES_" + missing not in os.environ
    if option == "PROTOCOL":
        # Exercise the SDK's default endpoint and HTTP path handling, without
        # connecting to a collector or installing global providers.
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

        exporter = OTLPSpanExporter()
        try:
            assert exporter._endpoint == "http://localhost:4318/v1/traces"
        finally:
            exporter.shutdown()


def test_specific_connection_overrides_generic(monkeypatch):
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

    monkeypatch.setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://generic:4317")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://specific:4318/custom")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
    _defaults.apply(exporting_config().telemetry)
    exporter = OTLPSpanExporter()
    try:
        assert exporter._endpoint == "http://specific:4318/custom"
        assert os.environ["OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"] == "http/protobuf"
    finally:
        exporter.shutdown()


@pytest.mark.parametrize("timeout", [0, 10000, 1234])
def test_timeout_omits_only_sdk_default(timeout):
    config = exporting_config().telemetry
    if timeout:
        config.traces.timeout.FromMilliseconds(timeout)
    _defaults.apply(config)
    if timeout in (0, 10000):
        assert "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT" not in os.environ
        assert _boot._resolve_otlp_timeout_seconds("TRACES") == 10
    else:
        assert _boot._resolve_otlp_timeout_seconds("TRACES") == 1.234
