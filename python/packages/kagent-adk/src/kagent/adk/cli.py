import asyncio
import json
import logging
import os
from typing import Annotated

import typer
import uvicorn
from a2a.types import AgentCard
from google.adk.cli.utils.agent_loader import AgentLoader

from kagent.core import KAgentConfig, configure_tracing

from . import AgentConfig, KAgentApp

logger = logging.getLogger(__name__)

app = typer.Typer()


@app.command()
def static(
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
    filepath: str = "/config",
    reload: Annotated[bool, typer.Option("--reload")] = False,
):
    configure_tracing()

    app_cfg = KAgentConfig()

    with open(os.path.join(filepath, "config.json"), "r") as f:
        config = json.load(f)
    agent_config = AgentConfig.model_validate(config)
    with open(os.path.join(filepath, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    root_agent = agent_config.to_agent(app_cfg.name)

    kagent_app = KAgentApp(root_agent, agent_card, app_cfg.url, app_cfg.app_name)

    uvicorn.run(
        kagent_app.build(),
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
    app_cfg = KAgentConfig()

    agent_loader = AgentLoader(agents_dir=working_dir)
    root_agent = agent_loader.load_agent(name)

    with open(os.path.join(working_dir, name, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    kagent_app = KAgentApp(root_agent, agent_card, app_cfg.url, app_cfg.app_name)
    uvicorn.run(
        kagent_app.build(),
        host=host,
        port=port,
        workers=workers,
    )


async def test_agent(agent_config: AgentConfig, agent_card: AgentCard, task: str):
    app_cfg = KAgentConfig()
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
