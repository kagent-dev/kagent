#! /usr/bin/env python3
import asyncio
import faulthandler
import logging
import os
from contextlib import asynccontextmanager
from typing import Any, Callable, List, Optional

import httpx
from a2a.server.apps import A2AFastAPIApplication
from a2a.server.events.event_queue import EventQueue
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard, Task, TaskStatusUpdateEvent, TaskState
from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.adk.agents import BaseAgent
from google.adk.apps import App
from google.adk.apps.app import EventsCompactionConfig, ResumabilityConfig
from google.adk.artifacts import InMemoryArtifactService
from google.adk.agents.context_cache_config import ContextCacheConfig as AdkContextCacheConfig
from google.adk.memory import BaseMemoryService, InMemoryMemoryService, VertexAiRagMemoryService
from google.adk.plugins import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.genai import types

from kagent.core.a2a import (
    KAgentRequestContextBuilder,
    KAgentTaskStore,
    get_a2a_max_content_length,
)

from ._agent_executor import A2aAgentExecutor, A2aAgentExecutorConfig
from ._lifespan import LifespanManager
from ._resumability import resume_tasks
from ._session_service import KAgentSessionService
from ._token import KAgentTokenService
from .memory import McpMemoryService
from .types import (
    BaseMemoryConfig,
    InMemoryConfig,
    McpMemoryConfig,
    VertexAIMemoryConfig,
    HttpMcpServerConfig,
    SseMcpServerConfig,
)
from google.adk.tools.mcp_tool import SseConnectionParams, StreamableHTTPConnectionParams

logger = logging.getLogger(__name__)


def health_check(request: Request) -> PlainTextResponse:
    return PlainTextResponse("OK")


def thread_dump(request: Request) -> PlainTextResponse:
    import io

    buf = io.StringIO()
    faulthandler.dump_traceback(file=buf)
    buf.seek(0)
    return PlainTextResponse(buf.read())


kagent_url_override = os.getenv("KAGENT_URL")


class KAgentApp:
    def __init__(
        self,
        root_agent_factory: Callable[[], BaseAgent],
        agent_card: AgentCard,
        kagent_url: str,
        app_name: str,
        lifespan: Optional[Callable[[Any], Any]] = None,
        plugins: List[BasePlugin] = None,
        stream: bool = False,
        events_compaction_config: Optional[EventsCompactionConfig] = None,
        context_cache_config: Optional[AdkContextCacheConfig] = None,
        memory_config: Optional[BaseMemoryConfig] = None,
        resumability_config: Optional[ResumabilityConfig] = None,
    ):
        """Initialize the KAgent application.

        Args:
            root_agent_factory: Root agent factory function that returns a new agent instance
            agent_card: Agent card configuration for A2A protocol
            kagent_url: URL of the KAgent backend server
            app_name: Application name for identification
            lifespan: Optional lifespan function
            plugins: Optional list of plugins
            stream: Whether to stream the response
            memory_config: Optional memory configuration
            resumability_config: Optional resumability configuration
        """
        self.root_agent_factory = root_agent_factory
        self.kagent_url = kagent_url
        self.app_name = app_name
        self.agent_card = agent_card
        self._lifespan = lifespan
        self.plugins = plugins if plugins is not None else []
        self.stream = stream
        self.events_compaction_config = events_compaction_config
        self.context_cache_config = context_cache_config
        self.memory_config = memory_config
        self.resumability_config = resumability_config

    def _create_memory_service(self) -> Optional[BaseMemoryService]:
        if not self.memory_config:
            return None

        if isinstance(self.memory_config, InMemoryConfig):
            return InMemoryMemoryService()
        elif isinstance(self.memory_config, VertexAIMemoryConfig):
            project_id = self.memory_config.project_id
            location = self.memory_config.location
            return VertexAiRagMemoryService(
                project_id=project_id,
                location=location,
            )
        elif isinstance(self.memory_config, McpMemoryConfig):
            config = self.memory_config.server_config
            if isinstance(config, HttpMcpServerConfig):
                # Ensure params are StreamableHTTPConnectionParams
                if not isinstance(config.params, StreamableHTTPConnectionParams):
                    # Should be handled by pydantic validation, but good to be safe
                    raise ValueError("Invalid params for HttpMcpServerConfig")
                return McpMemoryService(connection_params=config.params)
            elif isinstance(config, SseMcpServerConfig):
                if not isinstance(config.params, SseConnectionParams):
                    raise ValueError("Invalid params for SseMcpServerConfig")
                return McpMemoryService(connection_params=config.params)
            else:
                logger.warning(f"Unsupported MCP memory server config: {type(config)}")
                return None
        else:
            logger.warning(f"Unsupported memory config type: {type(self.memory_config)}")
            return None

    def build(self, local=False) -> FastAPI:
        session_service = InMemorySessionService()
        token_service = None
        http_client: Optional[httpx.AsyncClient] = None
        if not local:
            token_service = KAgentTokenService(self.app_name)
            http_client = httpx.AsyncClient(
                # TODO: add user  and agent headers
                base_url=kagent_url_override or self.kagent_url,
                event_hooks=token_service.event_hooks(),
            )
            session_service = KAgentSessionService(http_client)

        def create_runner() -> Runner:
            root_agent = self.root_agent_factory()
            adk_app = App(
                name=self.app_name,
                root_agent=root_agent,
                plugins=self.plugins,
                events_compaction_config=self.events_compaction_config,
                context_cache_config=self.context_cache_config,
                resumability_config=self.resumability_config,
            )

            return Runner(
                app=adk_app,
                session_service=session_service,
                artifact_service=InMemoryArtifactService(),
                memory_service=self._create_memory_service(),
            )

        task_store: InMemoryTaskStore | KAgentTaskStore = InMemoryTaskStore()
        if not local and http_client is not None:
            task_store = KAgentTaskStore(http_client)

        agent_executor = A2aAgentExecutor(
            runner=create_runner,
            config=A2aAgentExecutorConfig(stream=self.stream),
        )

        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        request_handler = DefaultRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            request_context_builder=request_context_builder,
        )

        max_content_length = get_a2a_max_content_length()
        a2a_app = A2AFastAPIApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
            max_content_length=max_content_length,
        )

        faulthandler.enable()

        @asynccontextmanager
        async def resume_lifespan(app: FastAPI):
            if not local and self.resumability_config and self.resumability_config.is_resumable:
                # We need to make sure agent_executor and task_store are available
                # They are available in the closure of build()
                if isinstance(task_store, KAgentTaskStore):
                    asyncio.create_task(resume_tasks(task_store, agent_executor))
            yield

        lifespan_manager = LifespanManager()
        lifespan_manager.add(self._lifespan)
        lifespan_manager.add(resume_lifespan)
        if not local:
            lifespan_manager.add(token_service.lifespan())

        app = FastAPI(lifespan=lifespan_manager)

        # Health check/readiness probe
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        a2a_app.add_routes_to_app(app)

        return app

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
            memory_service=self._create_memory_service(),
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
