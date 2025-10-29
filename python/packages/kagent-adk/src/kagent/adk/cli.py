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
from .sandbox_code_executer import SandboxedLocalCodeExecutor
from .skill_fetcher import fetch_skill

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
    code: Annotated[bool, typer.Option("--code")] = False,
):
    app_cfg = KAgentConfig()

    with open(os.path.join(filepath, "config.json"), "r") as f:
        config = json.load(f)
    agent_config = AgentConfig.model_validate(config)
    with open(os.path.join(filepath, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    code_executor = SandboxedLocalCodeExecutor() if code else None
    root_agent = agent_config.to_agent(app_cfg.name, code_executor=code_executor)

    kagent_app = KAgentApp(root_agent, agent_card, app_cfg.url, app_cfg.app_name)

    server = kagent_app.build()
    configure_tracing(server)

    uvicorn.run(
        server,
        host=host,
        port=port,
        workers=workers,
        reload=reload,
    )


@app.command()
def pull_skills(
    skills: Annotated[list[str], typer.Argument()],
):
    skill_dir = os.environ.get("SKILLS_FOLDER", ".")
    print("Pulling skills")
    for skill in skills:
        current_skill_dir = os.path.join(skill_dir, skill)
        print(f"Fetching skill {skill} into {current_skill_dir}")
        fetch_skill(skill, current_skill_dir)
    pass


@app.command()
def run(
    name: Annotated[str, typer.Argument(help="The name of the agent to run")],
    working_dir: str = ".",
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
    local: Annotated[
        bool, typer.Option("--local", help="Run with in-memory session service (for local development)")
    ] = False,
):
    app_cfg = KAgentConfig()

    agent_loader = AgentLoader(agents_dir=working_dir)
    root_agent = agent_loader.load_agent(name)

    with open(os.path.join(working_dir, name, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)

    kagent_app = KAgentApp(root_agent, agent_card, app_cfg.url, app_cfg.app_name)

    if local:
        logger.info("Running in local mode with InMemorySessionService")
        server = kagent_app.build_local()
    else:
        server = kagent_app.build()

    configure_tracing(server)

    uvicorn.run(
        server,
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
