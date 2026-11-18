#! /usr/bin/env python3
import faulthandler
import logging
from contextlib import AbstractAsyncContextManager
from typing import Callable, List, Optional

from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard
from google.adk.agents import BaseAgent
from google.adk.apps import App, ResumabilityConfig
from google.adk.apps.app import EventsCompactionConfig
from google.adk.artifacts import InMemoryArtifactService
from google.adk.plugins import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.genai import types
from kagent.core import AsyncControllerClient
from kagent.core._runtime_config import optional_field
from kagent.core.a2a import (
    A2AServer,
    KAgentRequestContextBuilder,
    TelemetryRequestHandler,
    attach_hitl_agent_extension,
    attach_usage_agent_extension,
)
from kagent.core.a2a._runtime import grpc_address
from kagent.core.a2a._task_store import KAgentRequestHandler, KAgentTaskStore

from ._agent_executor import A2aAgentExecutor, A2aAgentExecutorConfig
from ._local_session_service import LocalSessionService
from ._memory_service import KagentMemoryService
from ._token import KAgentTokenService
from .types import Config

logger = logging.getLogger(__name__)


class KAgentApp:
    def __init__(
        self,
        root_agent_factory: Callable[[], BaseAgent],
        agent_card: AgentCard,
        kagent_api_url: str,
        app_name: str,
        lifespan: Optional[Callable[[A2AServer], AbstractAsyncContextManager]] = None,
        plugins: Optional[List[BasePlugin]] = None,
        stream: bool = False,
        config: Optional[Config] = None,
        a2a_grpc_address: Optional[str] = None,
    ):
        """Initialize the KAgent application.

        Args:
            root_agent_factory: Root agent factory function that returns a new agent instance
            agent_card: Agent card configuration for A2A protocol
            kagent_api_url: URL of the KAgent control-plane API
            app_name: Application name for identification
            lifespan: Optional agent hook receiving the server before listeners start
            plugins: Optional list of plugins
            stream: Whether to stream the response
            config: Optional runtime and agent configuration
            a2a_grpc_address: Address for the A2A gRPC listener
        """
        self.root_agent_factory = root_agent_factory
        self.kagent_api_url = kagent_api_url
        self.config = config if config is not None else Config()
        self.a2a_grpc_address = a2a_grpc_address or grpc_address(self.config.runtime)
        self.app_name = app_name
        self.agent_card = agent_card
        self._lifespan = lifespan
        self.plugins = plugins if plugins is not None else []
        self.stream = stream

    def build(self, local=False) -> A2AServer:
        attach_hitl_agent_extension(self.agent_card)
        attach_usage_agent_extension(self.agent_card)
        session_service = InMemorySessionService()
        token_service = None
        controller_client: Optional[AsyncControllerClient] = None
        memory_service = None
        # Substrate sandbox agents with durable-dir session storage keep session state in a
        # local sqlite DB inside the actor's durableDir volume. The URL arrives as
        # AgentConfig.session_db_url in the rendered config (set by the controller).
        session_db_url = self.config.agent.session_db_url

        if not local:
            token_service = KAgentTokenService(self.app_name)
            controller_client = AsyncControllerClient(
                self.kagent_api_url,
                agent_name=self.app_name,
                token_provider=token_service,
            )
            if session_db_url:
                session_service = LocalSessionService(db_url=session_db_url)

            if self.config.agent.HasField("memory"):
                memory_service = KagentMemoryService(
                    controller_client=controller_client,
                    embedding_config=optional_field(self.config.agent.memory, "embedding"),
                    ttl_days=self.config.agent.memory.ttl_days,
                )

        def create_runner() -> Runner:
            root_agent = self.root_agent_factory()

            # Build ADK context config objects from agent config
            events_compaction_config: EventsCompactionConfig | None = None
            if self.config.agent.HasField("context_config"):
                from .types import build_adk_context_configs

                events_compaction_config, _ = build_adk_context_configs(self.config.agent.context_config)

            adk_app = App(
                name=self.app_name,
                root_agent=root_agent,
                plugins=self.plugins,
                events_compaction_config=events_compaction_config,
                resumability_config=ResumabilityConfig(is_resumable=True),
            )

            return Runner(
                app=adk_app,
                session_service=session_service,
                artifact_service=InMemoryArtifactService(),
                memory_service=memory_service,
            )

        task_store = KAgentTaskStore(controller_client) if controller_client else InMemoryTaskStore()

        agent_executor = A2aAgentExecutor(
            runner=create_runner,
            config=A2aAgentExecutorConfig(stream=self.stream),
        )

        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        handler_type = KAgentRequestHandler if controller_client else TelemetryRequestHandler
        request_handler = handler_type(
            agent_executor=agent_executor,
            task_store=task_store,
            agent_card=self.agent_card,
            request_context_builder=request_context_builder,
        )

        faulthandler.enable()

        return A2AServer(
            request_handler,
            self.config.runtime,
            token_service,
            controller_client,
            lifespan=self._lifespan,
            address=self.a2a_grpc_address,
        )

    async def test(self, task: str):
        session_service = InMemorySessionService()
        SESSION_ID = "12345"
        USER_ID = "admin"
        await session_service.create_session(
            app_name=self.app_name,
            session_id=SESSION_ID,
            user_id=USER_ID,
        )

        root_agent = self.root_agent_factory()
        runner = Runner(
            agent=root_agent,
            app_name=self.app_name,
            session_service=session_service,
            artifact_service=InMemoryArtifactService(),
        )

        logger.info(f"\n>>> User Query: {task}")

        # Prepare the user's message in ADK format
        content = types.Content(role="user", parts=[types.Part(text=task)])
        # Key Concept: run_async executes the agent logic and yields Events.
        # We iterate through events to find the final answer.
        async for event in runner.run_async(
            user_id=USER_ID,
            session_id=SESSION_ID,
            new_message=content,
        ):
            # You can uncomment the line below to see *all* events during execution
            # print(f"  [Event] Author: {event.author}, Type: {type(event).__name__}, Final: {event.is_final_response()}, Content: {event.content}")

            # Key Concept: is_final_response() marks the concluding message for the turn.
            jsn = event.model_dump_json()
            logger.info(f"  [Event] {jsn}")
