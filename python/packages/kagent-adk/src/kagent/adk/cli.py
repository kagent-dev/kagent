import asyncio
import importlib
import json
import logging
from pathlib import Path
from typing import Annotated, Optional

import typer
from a2a.types import AgentCard
from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin
from google.adk.agents import BaseAgent
from google.adk.cli.utils.agent_loader import AgentLoader
from google.protobuf.json_format import ParseDict
from kagent.core import KAgentConfig, bootstrap, configure_logging, materialize_from_bootstrap
from kagent.core._runtime_config import RuntimeConfig, TokenPropagationConfig
from kagent.core.telemetry import conv

from . import Config, KAgentApp
from .tools import add_builtin_tools_to_agent
from .types import create_agent, parse_config

logger = logging.getLogger(__name__)
logging.getLogger("google_adk.google.adk.tools.base_authenticated_tool").setLevel(logging.ERROR)

app = typer.Typer()


def create_sts_integration(settings: TokenPropagationConfig) -> Optional[ADKTokenPropagationPlugin]:
    if settings.sts_well_known_uri or settings.enabled:
        integration = ADKSTSIntegration(settings.sts_well_known_uri) if settings.sts_well_known_uri else None
        return ADKTokenPropagationPlugin(
            integration, resource=list(settings.resources) or None, audience=list(settings.audiences) or None
        )
    return None


def add_tools_with_config(root_agent: BaseAgent, config: Optional[Config] = None):
    runtime = config.runtime if config is not None else RuntimeConfig()

    def add_tools(agent: BaseAgent, agent_config=None):
        settings = runtime.builtin_tools
        add_builtin_tools_to_agent(
            agent_config.skills_directory if agent_config and agent_config.skills_directory else None,
            agent,
            disable_file_search=settings.disable_file_search,
            bash_venv_path=settings.bash_venv_path,
            disable_bash=settings.disable_bash,
        )
        child_configs = {child.name: child for child in agent_config.sub_agents} if agent_config else {}
        for child in agent.sub_agents:
            add_tools(child, child_configs.get(child.name))

    add_tools(root_agent, config.agent if config is not None else None)


def parse_agent_card(data: dict) -> AgentCard:
    return ParseDict(data, AgentCard())


def _load_agent_files(directory: Path) -> tuple[Config, AgentCard]:
    config = parse_config((directory / "config.json").read_text())
    card = parse_agent_card(json.loads((directory / "agent-card.json").read_text()))
    return config, card


def _load_revision(filepath: str) -> tuple[Config, AgentCard]:
    materialize_from_bootstrap(filepath)
    return _load_agent_files(Path(filepath))


_DEPRECATED = "[DEPRECATED] Ignored; A2A gRPC listens on runtime.host and runtime.port."
_Host = Annotated[Optional[str], typer.Option(help=_DEPRECATED)]
_Port = Annotated[Optional[int], typer.Option(help=_DEPRECATED)]
_Workers = Annotated[Optional[int], typer.Option(help=_DEPRECATED)]
_Reload = Annotated[bool, typer.Option("--reload", help=_DEPRECATED)]


def _warn_ignored_flags(**flags) -> None:
    for name, value in flags.items():
        if value:
            logger.warning("--%s is deprecated and ignored; A2A gRPC listens on runtime.host and runtime.port", name)


@app.command()
def static(
    filepath: str = "/config",
    host: _Host = None,
    port: _Port = None,
    workers: _Workers = None,
    reload: _Reload = False,
):
    config, agent_card = _load_revision(filepath)
    runtime = config.runtime
    app_cfg = KAgentConfig(runtime=runtime)
    configure_logging(runtime.log_level or "info")
    _warn_ignored_flags(host=host, port=port, workers=workers, reload=reload)
    plugins = None
    sts_integration = create_sts_integration(runtime.token_propagation)
    if sts_integration:
        plugins = [sts_integration]

    if config.agent.model.api_key_passthrough:
        from ._llm_passthrough_plugin import LLMPassthroughPlugin

        if plugins is None:
            plugins = []
        plugins.append(LLMPassthroughPlugin())

    def root_agent_factory() -> BaseAgent:
        root_agent = create_agent(
            config.agent, app_cfg.name, sts_integration, runtime.token_propagation.enabled, runtime_config=runtime
        )

        add_tools_with_config(root_agent, config)

        return root_agent

    kagent_app = KAgentApp(
        root_agent_factory,
        agent_card,
        app_cfg.api_url,
        app_cfg.app_name,
        plugins=plugins,
        stream=config.agent.stream,
        config=config,
    )

    server = kagent_app.build()
    bootstrap(conv.KagentRuntimeValues.ADK_PYTHON, fallback_name=agent_card.name, config=runtime)
    server.run()


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
    local: Annotated[
        bool, typer.Option("--local", help="Run with in-memory session service (for local development)")
    ] = False,
    host: _Host = None,
    port: _Port = None,
    workers: _Workers = None,
):
    if local:
        agent_dir = Path(working_dir) / name
        config_path = agent_dir / "config.json"
        config = parse_config(config_path.read_text()) if config_path.exists() else Config()
        agent_card = parse_agent_card(json.loads((agent_dir / "agent-card.json").read_text()))
        # Local runs are unprivileged; port 80 on every interface is the Substrate default.
        if not config.runtime.host:
            config.runtime.host = "127.0.0.1"
        if not config.runtime.port:
            config.runtime.port = 8080
    else:
        try:
            config, agent_card = _load_revision("/config")
        except RuntimeError as e:
            raise SystemExit(f"{e}. Outside Substrate, run with --local.") from e
    runtime = config.runtime
    app_cfg = KAgentConfig(runtime=runtime)
    configure_logging(runtime.log_level or "info")
    _warn_ignored_flags(host=host, port=port, workers=workers)

    plugins = None
    sts_integration = create_sts_integration(runtime.token_propagation)
    if sts_integration:
        plugins = [sts_integration]

    agent_loader = AgentLoader(agents_dir=working_dir)

    def root_agent_factory() -> BaseAgent:
        root_agent = agent_loader.load_agent(name)

        if sts_integration:
            add_to_agent(sts_integration, root_agent)

        add_tools_with_config(root_agent, config)

        return root_agent

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
        app_cfg.api_url,
        app_cfg.app_name,
        lifespan=lifespan,
        plugins=plugins,
        stream=config.agent.stream,
        config=config,
    )

    if local:
        logger.info("Running in local mode with InMemorySessionService")
        server = kagent_app.build(local=True)
    else:
        server = kagent_app.build()

    bootstrap(conv.KagentRuntimeValues.ADK_PYTHON, fallback_name=agent_card.name, config=runtime)
    server.run()


async def test_agent(config: Config, agent_card: AgentCard, task: str):
    app_cfg = KAgentConfig(
        api_url="http://fake-api.example.com",
        gateway_url="http://fake-gateway.example.com",
        name="test-agent",
        namespace="kagent",
    )
    runtime = config.runtime
    plugins = None
    sts_integration = create_sts_integration(runtime.token_propagation)
    if sts_integration:
        plugins = [sts_integration]

    def root_agent_factory() -> BaseAgent:
        root_agent = create_agent(
            config.agent, app_cfg.name, sts_integration, runtime.token_propagation.enabled, runtime_config=runtime
        )
        add_tools_with_config(root_agent, config)
        return root_agent

    app = KAgentApp(root_agent_factory, agent_card, app_cfg.api_url, app_cfg.app_name, plugins=plugins, config=config)
    await app.test(task)


@app.command()
def test(
    task: Annotated[str, typer.Option("--task", help="The task to test the agent with")],
    filepath: Annotated[str, typer.Option("--filepath", help="The path to the agent config file")],
):
    config, agent_card = _load_agent_files(Path(filepath))
    asyncio.run(test_agent(config, agent_card, task))


def run_cli():
    configure_logging()
    logger.info("Starting KAgent")
    app()


if __name__ == "__main__":
    run_cli()
