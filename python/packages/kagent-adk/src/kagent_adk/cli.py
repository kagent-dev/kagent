import asyncio
import json
import logging
import os
from typing import Annotated, Literal

import aiofiles
import typer
import uvicorn
from a2a.types import AgentCard
from google.adk.cli.utils.agent_loader import AgentLoader
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from . import AgentConfig, KAgentApp

logger = logging.getLogger(__name__)

app = typer.Typer()

kagent_url = os.getenv("KAGENT_URL")
kagent_name = os.getenv("KAGENT_NAME")
kagent_namespace = os.getenv("KAGENT_NAMESPACE")


@app.command()
def static(
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
    filepath: str = "/config",
    reload: Annotated[bool, typer.Option("--reload")] = False,
):
    tracing_enabled = os.getenv("OTEL_TRACING_ENABLED", "false").lower() == "true"
    if tracing_enabled:
        logging.info("Enabling tracing")
        tracer_provider = TracerProvider(resource=Resource({"service.name": "kagent"}))
        processor = BatchSpanProcessor(OTLPSpanExporter())
        tracer_provider.add_span_processor(processor)
        trace.set_tracer_provider(tracer_provider)
        HTTPXClientInstrumentor().instrument()
        OpenAIInstrumentor().instrument()

    if not kagent_url:
        raise ValueError("KAGENT_URL is not set")
    if not kagent_name:
        raise ValueError("KAGENT_NAME is not set")
    if not kagent_namespace:
        raise ValueError("KAGENT_NAMESPACE is not set")

    with open(os.path.join(filepath, "config.json"), "r") as f:
        config = json.load(f)
    agent_config = AgentConfig.model_validate(config)
    with open(os.path.join(filepath, "agent_card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    root_agent = agent_config.to_agent()

    kagent_app = KAgentApp(root_agent, agent_card, kagent_url, kagent_name + "__NS__" + kagent_namespace)

    uvicorn.run(
        kagent_app.build,
        host=host,
        port=port,
        workers=workers,
        reload=reload,
    )


@app.command()
def run(
    name: str,
    working_dir: str = ".",
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
):
    agent_loader = AgentLoader(agents_dir=working_dir)
    root_agent = agent_loader.load_agent(name)
    if not kagent_url:
        raise ValueError("KAGENT_URL is not set")
    if not kagent_name:
        raise ValueError("KAGENT_NAME is not set")
    if not kagent_namespace:
        raise ValueError("KAGENT_NAMESPACE is not set")

    with open(os.path.join(working_dir, name, "agent_card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    kagent_app = KAgentApp(root_agent, agent_card, kagent_url, kagent_name + "__NS__" + kagent_namespace)
    uvicorn.run(
        kagent_app.build,
        host=host,
        port=port,
        workers=workers,
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


def run_cli():
    logging.basicConfig(level=logging.INFO)
    logging.info("Starting KAgent")
    app()


if __name__ == "__main__":
    run_cli()
