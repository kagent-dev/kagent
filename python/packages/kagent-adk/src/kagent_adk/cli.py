import asyncio
import json
import logging
import os
from typing import Annotated

import typer
import uvicorn
from a2a.types import AgentCard
from google.adk.agents import BaseAgent
from google.adk.cli.utils.agent_loader import AgentLoader
from openai import BaseModel
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
    name: str,
    working_dir: str = ".",
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
):
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
