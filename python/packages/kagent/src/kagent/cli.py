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
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor
from opentelemetry.instrumentation.openai import OpenAIInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

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
    if tracing_enabled:
        logging.info("Enabling tracing")
        tracer_provider = TracerProvider(resource=Resource({"service.name": "kagent"}))
        processor = BatchSpanProcessor(OTLPSpanExporter())
        tracer_provider.add_span_processor(processor)
        trace.set_tracer_provider(tracer_provider)
        HTTPXClientInstrumentor().instrument()
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
