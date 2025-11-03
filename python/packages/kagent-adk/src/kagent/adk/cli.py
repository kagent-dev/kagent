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
from .skill_fetcher import fetch_skill

from . import AgentConfig, KAgentApp
from .skills.skills_plugin import add_skills_tool_to_agent

logger = logging.getLogger(__name__)
logging.getLogger("google_adk.google.adk.tools.base_authenticated_tool").setLevel(logging.ERROR)

app = typer.Typer()


@app.command()
def static(
    host: str = "127.0.0.1",
    port: int = 8080,
    workers: int = 1,
    filepath: str = "/config",
    reload: Annotated[bool, typer.Option("--reload")] = False,
):
    app_cfg = KAgentConfig()

    with open(os.path.join(filepath, "config.json"), "r") as f:
        config = json.load(f)
    agent_config = AgentConfig.model_validate(config)
    with open(os.path.join(filepath, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    root_agent = agent_config.to_agent(app_cfg.name)
    skills_directory = os.getenv("KAGENT_SKILLS_FOLDER", None)
    if skills_directory:
        logger.info(f"Adding skills from directory: {skills_directory}")
        add_skills_tool_to_agent(skills_directory, root_agent)

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
    insecure: Annotated[
        bool,
        typer.Option("--insecure", help="Allow insecure connections to registries"),
    ] = False,
):
    skill_dir = os.environ.get("KAGENT_SKILLS_FOLDER", ".")
    logger.info("Pulling skills")
    for skill in skills:
        fetch_skill(skill, skill_dir, insecure)


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


# --- Configure Logging ---
def configure_logging() -> None:
    """Configure logging based on LOG_LEVEL environment variable."""
    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    logging.basicConfig(
        level=log_level,
    )
    logging.info(f"Logging configured with level: {log_level}")


def run_cli():
    configure_logging()
    logger.info("Starting KAgent")
    app()


if __name__ == "__main__":
    run_cli()
