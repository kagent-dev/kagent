import asyncio
import json
import logging
import os
from typing import Annotated

import typer
import uvicorn
from a2a.types import AgentCard
from google.adk.cli.utils.agent_loader import AgentLoader
from opentelemetry import _logs, trace
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.anthropic import AnthropicInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.sdk._events import EventLoggerProvider
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from . import AgentConfig, KAgentApp

logger = logging.getLogger(__name__)

app = typer.Typer()

kagent_url = os.getenv("KAGENT_URL")
kagent_name = os.getenv("KAGENT_NAME")
kagent_namespace = os.getenv("KAGENT_NAMESPACE")


class Config:
    _url: str
    _name: str
    _namespace: str

    def __init__(self):
        if not kagent_url:
            raise ValueError("KAGENT_URL is not set")
        if not kagent_name:
            raise ValueError("KAGENT_NAME is not set")
        if not kagent_namespace:
            raise ValueError("KAGENT_NAMESPACE is not set")
        self._url = kagent_url
        self._name = kagent_name
        self._namespace = kagent_namespace

    @property
    def name(self):
        return self._name.replace("-", "_")

    @property
    def namespace(self):
        return self._namespace.replace("-", "_")

    @property
    def app_name(self):
        return self.namespace + "__NS__" + self.name

    @property
    def url(self):
        return self._url


def configure_tracing():
    tracing_enabled = os.getenv("OTEL_TRACING_ENABLED", "false").lower() == "true"
    logging_enabled = os.getenv("OTEL_LOGGING_ENABLED", "false").lower() == "true"

    resource = Resource({"service.name": "kagent"})

    # Configure tracing if enabled
    if tracing_enabled:
        logging.info("Enabling tracing")
        tracer_provider = TracerProvider(resource=resource)
        # Check new env var first, fall back to old one for backward compatibility
        trace_endpoint = os.getenv("OTEL_TRACING_EXPORTER_OTLP_ENDPOINT") or os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
        if trace_endpoint:
            processor = BatchSpanProcessor(OTLPSpanExporter(endpoint=trace_endpoint))
        else:
            processor = BatchSpanProcessor(OTLPSpanExporter())
        tracer_provider.add_span_processor(processor)
        trace.set_tracer_provider(tracer_provider)

    # Configure logging if enabled
    if logging_enabled:
        logging.info("Enabling logging for GenAI events")
        logger_provider = LoggerProvider(resource=resource)
        log_endpoint = os.getenv("OTEL_LOGGING_EXPORTER_OTLP_ENDPOINT")
        logging.info(f"Log endpoint configured: {log_endpoint}")

        # Add OTLP exporter
        if log_endpoint:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter(endpoint=log_endpoint))
        else:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter())
        logger_provider.add_log_record_processor(log_processor)

        _logs.set_logger_provider(logger_provider)
        logging.info("Log provider configured with OTLP")
        # When logging is enabled, use new event-based approach (input/output as log events in Body)
        logging.info("OpenAI instrumentation configured with event logging capability")
        # Create event logger provider using the configured logger provider
        event_logger_provider = EventLoggerProvider(logger_provider)
        OpenAIInstrumentor(use_legacy_attributes=False).instrument(event_logger_provider=event_logger_provider)
        AnthropicInstrumentor(use_legacy_attributes=False).instrument(event_logger_provider=event_logger_provider)
    else:
        # Use legacy attributes (input/output as GenAI span attributes)
        logging.info("OpenAI instrumentation configured with legacy GenAI span attributes")
        OpenAIInstrumentor().instrument()
        AnthropicInstrumentor().instrument()


@app.command()
def static(
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
    filepath: str = "/config",
    reload: Annotated[bool, typer.Option("--reload")] = False,
):
    configure_tracing()

    app_cfg = Config()

    with open(os.path.join(filepath, "config.json"), "r") as f:
        config = json.load(f)
    agent_config = AgentConfig.model_validate(config)
    with open(os.path.join(filepath, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    root_agent = agent_config.to_agent(app_cfg.name)

    kagent_app = KAgentApp(root_agent, agent_card, app_cfg.url, app_cfg.app_name)

    uvicorn.run(
        kagent_app.build,
        host=host,
        port=port,
        workers=workers,
        reload=reload,
    )


@app.command()
def run(
    name: Annotated[str, typer.Argument(help="The name of the agent to run")],
    working_dir: str = ".",
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
):
    configure_tracing()
    app_cfg = Config()

    agent_loader = AgentLoader(agents_dir=working_dir)
    root_agent = agent_loader.load_agent(name)

    with open(os.path.join(working_dir, name, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    kagent_app = KAgentApp(root_agent, agent_card, app_cfg.url, app_cfg.app_name)
    uvicorn.run(
        kagent_app.build,
        host=host,
        port=port,
        workers=workers,
    )


async def test_agent(agent_config: AgentConfig, agent_card: AgentCard, task: str):
    app_cfg = Config()
    agent = agent_config.to_agent(app_cfg.name)
    app = KAgentApp(agent, agent_card, app_cfg.url, app_cfg.app_name)
    await app.test(task)


@app.command()
def test(
    task: Annotated[str, typer.Option("--task", help="The task to test the agent with")],
    filepath: Annotated[str, typer.Option("--filepath", help="The path to the agent config file")],
):
    with open(filepath, "r") as f:
        content = f.read()
        config = json.loads(content)

    with open(os.path.join(filepath, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    agent_config = AgentConfig.model_validate(config)
    asyncio.run(test_agent(agent_config, agent_card, task))


def run_cli():
    logging.basicConfig(level=logging.INFO)
    logging.info("Starting KAgent")
    app()


if __name__ == "__main__":
    run_cli()
