import logging
import os
from contextlib import asynccontextmanager
from dataclasses import dataclass
from typing import Any, Callable

from fastapi import FastAPI
from opentelemetry import _logs, metrics, trace
from opentelemetry.distro import OpenTelemetryConfigurator
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.propagate import set_global_textmap
from opentelemetry.propagators.composite import CompositePropagator
from opentelemetry.sdk.resources import OTELResourceDetector
from opentelemetry.semconv.attributes.service_attributes import SERVICE_NAME
from opentelemetry.util._importlib_metadata import entry_points

from . import _defaults
from ._conv import GEN_AI_AGENT_ID, GEN_AI_AGENT_NAME, KAGENT_RUNTIME, KagentRuntimeValues
from ._profiles import COMMON_INSTRUMENTORS, Profile, profile_for
from ._span_processor import KagentAttributesSpanProcessor

logger = logging.getLogger(__name__)

SCHEMA_URL = "https://opentelemetry.io/schemas/1.43.0"

# OTEL_BSP_EXPORT_TIMEOUT defaults to 30s and must not bound a response tail.
FLUSH_TIMEOUT_MILLIS = 3000

_SERVICE_NAMESPACE = "service.namespace"


@dataclass(frozen=True)
class Identity:
    runtime: KagentRuntimeValues
    agent_name: str
    agent_namespace: str

    @property
    def agent_id(self) -> str:
        if not self.agent_name or not self.agent_namespace:
            return ""
        return f"{self.agent_namespace}/{self.agent_name}"

    def attributes(self) -> dict[str, str]:
        attributes = {GEN_AI_AGENT_NAME: self.agent_name, GEN_AI_AGENT_ID: self.agent_id}
        return {key: value for key, value in attributes.items() if value}


@dataclass(frozen=True)
class Providers:
    identity: Identity
    profile: Profile

    def force_flush(self, timeout_millis: int = FLUSH_TIMEOUT_MILLIS) -> None:
        force_flush(timeout_millis)

    def shutdown(self) -> None:
        for provider in (_logs.get_logger_provider(), trace.get_tracer_provider(), metrics.get_meter_provider()):
            shutdown = getattr(provider, "shutdown", None)
            if shutdown is None:
                continue
            try:
                shutdown()
            except Exception:
                logger.warning("Failed to shut down a telemetry provider", exc_info=True)


_current: Providers | None = None


def current() -> Providers | None:
    return _current


def signal_enabled(signal: str) -> bool:
    """Report whether a signal exports, reading unset as otlp like the SDK."""
    if os.getenv("OTEL_SDK_DISABLED", "").strip().lower() == "true":
        return False
    exporters = os.getenv(f"OTEL_{signal}_EXPORTER") or "otlp"
    return any(exporter.strip().lower() not in ("", "none") for exporter in exporters.split(","))


def force_flush(timeout_millis: int = FLUSH_TIMEOUT_MILLIS) -> None:
    """Export buffered logs, spans and metrics before the process may be suspended."""
    for provider in (_logs.get_logger_provider(), trace.get_tracer_provider(), metrics.get_meter_provider()):
        flush = getattr(provider, "force_flush", None)
        if flush is None:
            continue
        try:
            flush(timeout_millis)
        except Exception:
            logger.warning("Failed to flush pending telemetry", exc_info=True)


def _resolve_otlp_timeout_seconds(signal: str) -> float:
    """Read OTEL_EXPORTER_OTLP_*TIMEOUT as milliseconds, which the Python exporters read as seconds."""
    name = f"OTEL_EXPORTER_OTLP_{signal}_TIMEOUT"
    raw = os.getenv(name) or os.getenv("OTEL_EXPORTER_OTLP_TIMEOUT")
    if raw is None:
        return 10.0
    try:
        millis = float(raw)
    except ValueError:
        logger.warning("Invalid OTLP timeout %r from %s; using 10000ms", raw, name)
        return 10.0
    if millis < 0:
        logger.warning("Negative OTLP timeout %r from %s; using 10000ms", raw, name)
        return 10.0
    return millis / 1000.0


def _exporter_args() -> dict[type, dict[str, Any]]:
    from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter as GrpcLogExporter
    from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter as GrpcMetricExporter
    from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter as GrpcSpanExporter
    from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter as HttpLogExporter
    from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter as HttpMetricExporter
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter as HttpSpanExporter

    exporters = {
        "TRACES": (GrpcSpanExporter, HttpSpanExporter),
        "METRICS": (GrpcMetricExporter, HttpMetricExporter),
        "LOGS": (GrpcLogExporter, HttpLogExporter),
    }
    return {
        exporter: {"timeout": _resolve_otlp_timeout_seconds(signal)}
        for signal, classes in exporters.items()
        for exporter in classes
    }


def _environment_propagator() -> CompositePropagator:
    """Build the propagator OTEL_PROPAGATORS names, even when opentelemetry.propagate was imported first."""
    names = [name.strip() for name in os.environ.get("OTEL_PROPAGATORS", "").split(",") if name.strip()]
    if "none" in names:
        return CompositePropagator([])
    return CompositePropagator(
        [next(iter(entry_points(group="opentelemetry_propagator", name=name))).load()() for name in names]
    )


def _resource_attributes(identity: Identity) -> dict[str, str]:
    environment = OTELResourceDetector().detect().attributes
    attributes: dict[str, str] = {}
    if identity.agent_name and SERVICE_NAME not in environment:
        attributes[SERVICE_NAME] = identity.agent_name
    if identity.agent_namespace and _SERVICE_NAMESPACE not in environment:
        attributes[_SERVICE_NAMESPACE] = identity.agent_namespace
    attributes[KAGENT_RUNTIME] = identity.runtime.value
    return attributes


def _instrument(names: tuple[str, ...], options: dict[str, dict[str, Any]]) -> None:
    disabled = {name.strip() for name in os.getenv("OTEL_PYTHON_DISABLED_INSTRUMENTATIONS", "").split(",")}
    for name in names:
        if name in disabled:
            continue
        found = list(entry_points(group="opentelemetry_instrumentor", name=name))
        if not found:
            logger.warning("OpenTelemetry instrumentation %s is not installed", name)
            continue
        try:
            found[0].load()().instrument(**options.get(name, {}))
        except Exception:
            logger.warning("Failed to install OpenTelemetry instrumentation %s", name, exc_info=True)


def bootstrap(
    runtime: KagentRuntimeValues,
    fallback_name: str | None = None,
    instrument_options: dict[str, dict[str, Any]] | None = None,
) -> Providers:
    """Install the SDK from the environment and the runtime's instrumentations.

    Identity follows the Go runtimes: KAGENT_NAME, else fallback_name, and
    KAGENT_NAMESPACE, else "default". Configuration errors never stop startup.
    """
    global _current
    if _current is not None:
        return _current
    profile = profile_for(runtime)
    identity = Identity(
        runtime=runtime,
        agent_name=os.getenv("KAGENT_NAME") or fallback_name or "",
        agent_namespace=os.getenv("KAGENT_NAMESPACE") or "default",
    )
    _current = Providers(identity=identity, profile=profile)
    _defaults.apply()
    set_global_textmap(_environment_propagator())
    if os.getenv("OTEL_SDK_DISABLED", "").strip().lower() == "true":
        return _current
    try:
        OpenTelemetryConfigurator().configure(
            resource_attributes=_resource_attributes(identity),
            span_processors=[KagentAttributesSpanProcessor()],
            exporter_args_map=_exporter_args(),
        )
    except Exception:
        logger.exception("Failed to configure OpenTelemetry; telemetry is disabled")
        return _current
    if signal_enabled("TRACES") or signal_enabled("METRICS") or signal_enabled("LOGS"):
        _instrument(COMMON_INSTRUMENTORS + profile.instrumentors, instrument_options or {})
    return _current


def instrument_app(app: FastAPI, **providers: Any) -> None:
    if not (signal_enabled("TRACES") or signal_enabled("METRICS")):
        return
    FastAPIInstrumentor.instrument_app(app, exclude_spans=["receive", "send"], **providers)


def shutdown_lifespan(inner: Callable[[FastAPI], Any] | None = None):
    """Wrap a FastAPI lifespan so the providers shut down when the app stops."""

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        try:
            if inner is None:
                yield
            else:
                async with inner(app):
                    yield
        finally:
            if _current is not None:
                _current.shutdown()

    return lifespan
