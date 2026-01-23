import asyncio
import importlib
import json
import logging
import os
from typing import Annotated, Optional

import typer
import uvicorn
from a2a.types import AgentCard
from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin
from google.adk.agents import BaseAgent
from google.adk.cli.utils.agent_loader import AgentLoader

from kagent.core import KAgentConfig, configure_logging, configure_tracing

from . import AgentConfig, KAgentApp
from .skill_fetcher import fetch_skill
from .tools import add_skills_tool_to_agent

logger = logging.getLogger(__name__)
logging.getLogger("google_adk.google.adk.tools.base_authenticated_tool").setLevel(logging.ERROR)

app = typer.Typer()


kagent_url_override = os.getenv("KAGENT_URL")
sts_well_known_uri = os.getenv("STS_WELL_KNOWN_URI")
propagate_token = os.getenv("KAGENT_PROPAGATE_TOKEN")


def create_sts_integration() -> Optional[ADKTokenPropagationPlugin]:
    if sts_well_known_uri or propagate_token:
        sts_integration = None
        if sts_well_known_uri:
            sts_integration = ADKSTSIntegration(sts_well_known_uri)
        return ADKTokenPropagationPlugin(sts_integration)


def maybe_add_skills(root_agent: BaseAgent):
    skills_directory = os.getenv("KAGENT_SKILLS_FOLDER", None)
    if skills_directory:
        logger.info(f"Adding skills from directory: {skills_directory}")
        add_skills_tool_to_agent(skills_directory, root_agent)


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
    plugins = None
    sts_integration = create_sts_integration()
    if sts_integration:
        plugins = [sts_integration]

    def root_agent_factory() -> BaseAgent:
        root_agent = agent_config.to_agent(app_cfg.name, sts_integration)

        maybe_add_skills(root_agent)

        return root_agent

    kagent_app = KAgentApp(
        root_agent_factory,
        agent_card,
        app_cfg.url,
        app_cfg.app_name,
        plugins=plugins,
        stream=agent_config.stream if agent_config.stream is not None else False,
    )

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


def add_to_agent(sts_integration: ADKTokenPropagationPlugin, agent: BaseAgent):
    """
    Add the plugin to an ADK LLM agent by updating its MCP toolset
    Call this once when setting up the agent; do not call it at runtime.
    """
    from google.adk.agents import LlmAgent
    from google.adk.tools.mcp_tool.mcp_toolset import McpToolset

    if not isinstance(agent, LlmAgent):
        return

    if not agent.tools:
        return

    for tool in agent.tools:
        if isinstance(tool, McpToolset):
            mcp_toolset = tool
            mcp_toolset._header_provider = sts_integration.header_provider
            logger.debug("Updated tool connection params to include access token from STS server")


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

    plugins = None
    sts_integration = create_sts_integration()
    if sts_integration:
        plugins = [sts_integration]

    agent_loader = AgentLoader(agents_dir=working_dir)

    def root_agent_factory() -> BaseAgent:
        root_agent = agent_loader.load_agent(name)

        if sts_integration:
            add_to_agent(sts_integration, root_agent)

        maybe_add_skills(root_agent)

        return root_agent

    # Load agent config to get stream setting
    agent_config = None
    config_path = os.path.join(working_dir, name, "config.json")
    try:
        with open(config_path, "r") as f:
            config = json.load(f)
        agent_config = AgentConfig.model_validate(config)
    except FileNotFoundError:
        logger.debug(f"No config.json found at {config_path}, using defaults")

    with open(os.path.join(working_dir, name, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)

    # Attempt to import optional user-defined lifespan(app) from the agent package
    lifespan = None
    try:
        module_candidate = importlib.import_module(name)
        if hasattr(module_candidate, "lifespan"):
            lifespan = module_candidate.lifespan
    except Exception:
        logger.exception(f"Failed to load agent module '{name}' for lifespan")

    kagent_app = KAgentApp(
        root_agent_factory,
        agent_card,
        app_cfg.url,
        app_cfg.app_name,
        lifespan=lifespan,
        plugins=plugins,
        stream=agent_config.stream if agent_config and agent_config.stream is not None else False,
    )

    if local:
        logger.info("Running in local mode with InMemorySessionService")
        server = kagent_app.build(local=True)
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
    app_cfg = KAgentConfig(url="http://fake-url.example.com", name="test-agent", namespace="kagent")
    plugins = None
    sts_integration = create_sts_integration()
    if sts_integration:
        plugins = [sts_integration]

    def root_agent_factory() -> BaseAgent:
        root_agent = agent_config.to_agent(app_cfg.name, sts_integration)
        maybe_add_skills(root_agent)
        return root_agent

    app = KAgentApp(root_agent_factory, agent_card, app_cfg.url, app_cfg.app_name, plugins=plugins)
    await app.test(task)


@app.command()
def test(
    task: Annotated[str, typer.Option("--task", help="The task to test the agent with")],
    filepath: Annotated[str, typer.Option("--filepath", help="The path to the agent config file")],
):
    with open(os.path.join(filepath, "config.json"), "r") as f:
        content = f.read()
        config = json.loads(content)

    with open(os.path.join(filepath, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    agent_config = AgentConfig.model_validate(config)
    asyncio.run(test_agent(agent_config, agent_card, task))


def run_cli():
    configure_logging()
    logger.info("Starting KAgent")
    app()


if __name__ == "__main__":
    run_cli()
