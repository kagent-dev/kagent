import asyncio
import json
import logging
import os
from typing import Annotated

import aiofiles
import typer
import uvicorn
from kagent_adk import AgentConfig, KAgentApp
from opentelemetry import trace
from opentelemetry import _logs
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk._logs.export import ConsoleLogExporter
from opentelemetry.sdk._events import EventLoggerProvider
from opentelemetry._logs import get_logger_provider

logger = logging.getLogger(__name__)

app = typer.Typer()


@app.command()
def static(
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
    filepath: str = "/config/config.json",
    reload: Annotated[bool, typer.Option("--reload")] = False,
):
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
        HTTPXClientInstrumentor().instrument()

    # Configure logging if enabled
    if logging_enabled:
        logging.info("Enabling logging for GenAI events")
        logger_provider = LoggerProvider(resource=resource)
        log_endpoint = os.getenv("OTEL_LOGGING_EXPORTER_OTLP_ENDPOINT")
        logging.info(f"Log endpoint configured: {log_endpoint}")

        if log_endpoint:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter(endpoint=log_endpoint))
        else:
            log_processor = BatchLogRecordProcessor(OTLPLogExporter())
        logger_provider.add_log_record_processor(log_processor)

        _logs.set_logger_provider(logger_provider)
        logging.info("Log provider configured with OTLP exporter")

    if tracing_enabled or logging_enabled:
        if logging_enabled:
            # When logging is enabled, use new event-based approach (input/output as log events in Body)
            logging.info("OpenAI instrumentation configured with event logging capability")
            event_logger_provider = EventLoggerProvider(get_logger_provider())
            OpenAIInstrumentor(use_legacy_attributes=False).instrument(event_logger_provider=event_logger_provider)
        else:
            # Otherwise, use legacy attributes (input/output as GenAI span attributes)
            logging.info("OpenAI instrumentation configured with legacy GenAI span attributes")
            OpenAIInstrumentor().instrument()

    with open(filepath, "r") as f:
        config = json.load(f)
    agent_config = AgentConfig.model_validate(config)
    root_agent = agent_config.to_agent()

    app = KAgentApp(root_agent, agent_config.agent_card, agent_config.kagent_url, agent_config.name)

    uvicorn.run(
        app.build,
        host=host,
        port=port,
        workers=workers,
        reload=reload,
    )


async def test_agent(filepath: str, task: str):
    async with aiofiles.open(filepath, "r") as f:
        content = await f.read()
        config = json.loads(content)
    agent_config = AgentConfig.model_validate(config)
    agent = agent_config.to_agent()

    app = KAgentApp(agent, agent_config.agent_card, agent_config.kagent_url, agent_config.name)
    await app.test(task)


@app.command()
def test(
    task: Annotated[str, typer.Option("--task", help="The task to test the agent with")],
    filepath: Annotated[str, typer.Option("--filepath", help="The path to the agent config file")],
):
    asyncio.run(test_agent(filepath, task))


def run():
    logging.basicConfig(level=logging.INFO)
    logging.info("Starting KAgent")
    app()


if __name__ == "__main__":
    run()
