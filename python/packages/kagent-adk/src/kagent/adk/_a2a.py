#! /usr/bin/env python3
import faulthandler
import logging
import os
from contextlib import asynccontextmanager
from typing import Any, Callable, List, Optional

import grpc
from a2a.server.request_handlers import DefaultRequestHandlerV2
from a2a.server.request_handlers.grpc_handler import GrpcHandler
from a2a.server.routes import add_a2a_routes_to_fastapi, create_agent_card_routes, create_jsonrpc_routes
from a2a.server.tasks import InMemoryTaskStore, TaskStore
from a2a.types import AgentCard, a2a_pb2, a2a_pb2_grpc
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.adk.agents import BaseAgent
from google.adk.apps import App, ResumabilityConfig
from google.adk.apps.app import EventsCompactionConfig
from google.adk.artifacts import InMemoryArtifactService
from google.adk.plugins import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import DatabaseSessionService, InMemorySessionService
from google.genai import types
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from kagent.core import AsyncControllerClient
from kagent.core.a2a import (
    A2ARequestSizeLimitMiddleware,
    KAgentGrpcServerCallContextBuilder,
    KAgentRequestContextBuilder,
    KAgentTaskStore,
    attach_hitl_agent_extension,
    get_a2a_max_content_length,
)

from ._agent_executor import A2aAgentExecutor, A2aAgentExecutorConfig
from ._lifespan import LifespanManager
from ._memory_service import KagentMemoryService
from ._session_service import KAgentSessionService
from ._token import KAgentTokenService
from .types import AgentConfig

logger = logging.getLogger(__name__)


def health_check(request: Request) -> PlainTextResponse:
    return PlainTextResponse("OK")


def thread_dump(request: Request) -> PlainTextResponse:
    import tempfile

    with tempfile.TemporaryFile(mode="w+") as tmp:
        faulthandler.dump_traceback(file=tmp, all_threads=True)
        tmp.seek(0)
        return PlainTextResponse(tmp.read())


class KAgentApp:
    def __init__(
        self,
        root_agent_factory: Callable[[], BaseAgent],
        agent_card: AgentCard,
        kagent_url: str,
        app_name: str,
        lifespan: Optional[Callable[[Any], Any]] = None,
        plugins: Optional[List[BasePlugin]] = None,
        stream: bool = False,
        agent_config: Optional[AgentConfig] = None,
        kagent_grpc_url: Optional[str] = None,
        a2a_grpc_address: Optional[str] = None,
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
            agent_config: Optional agent configuration
            a2a_grpc_address: Address for the A2A gRPC listener
        """
        self.root_agent_factory = root_agent_factory
        self.kagent_url = kagent_url
        self.kagent_grpc_url = kagent_grpc_url or os.getenv("KAGENT_GRPC_URL")
        self.a2a_grpc_address = a2a_grpc_address or os.getenv("KAGENT_A2A_GRPC_ADDRESS", "[::]:80")
        self.app_name = app_name
        self.agent_card = agent_card
        self._lifespan = lifespan
        self.plugins = plugins if plugins is not None else []
        self.stream = stream
        self.agent_config = agent_config

    def build(self, local=False) -> FastAPI:
        attach_hitl_agent_extension(self.agent_card)
        session_service = InMemorySessionService()
        token_service = None
        controller_client: Optional[AsyncControllerClient] = None
        memory_service = None
        # Substrate sandbox agents with durable-dir session storage keep session state in a
        # local sqlite DB inside the actor's durableDir volume. The URL arrives as
        # AgentConfig.session_db_url in the rendered config (set by the controller).
        session_db_url = self.agent_config.session_db_url if self.agent_config else None

        if not local:
            if not self.kagent_grpc_url:
                raise ValueError("KAGENT_GRPC_URL environment variable is not set")
            token_service = KAgentTokenService(self.app_name)
            controller_client = AsyncControllerClient(
                self.kagent_grpc_url,
                agent_name=self.app_name,
                token_provider=token_service,
            )
            if session_db_url:
                session_service = DatabaseSessionService(db_url=session_db_url)
            else:
                session_service = KAgentSessionService(controller_client)

            if self.agent_config and self.agent_config.memory is not None:
                memory_service = KagentMemoryService(
                    agent_name=self.app_name,
                    controller_client=controller_client,
                    embedding_config=self.agent_config.memory.embedding,
                    ttl_days=self.agent_config.memory.ttl_days,
                )

        def create_runner() -> Runner:
            root_agent = self.root_agent_factory()

            if not local and controller_client is not None and self.agent_config and self.agent_config.share_tools:
                from kagent.adk.tools.share_tools import CreateShareLinkTool, DeleteShareLinkTool, ListShareLinksTool

                root_agent.tools.extend(
                    [
                        CreateShareLinkTool(controller_client),
                        ListShareLinksTool(controller_client),
                        DeleteShareLinkTool(controller_client),
                    ]
                )

            # Build ADK context config objects from agent config
            events_compaction_config: EventsCompactionConfig | None = None
            if self.agent_config and self.agent_config.context_config is not None:
                from .types import build_adk_context_configs

                events_compaction_config, _ = build_adk_context_configs(self.agent_config.context_config)

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

        task_store: TaskStore = InMemoryTaskStore()
        if not local and controller_client is not None:
            task_store = KAgentTaskStore(controller_client)

        agent_executor = A2aAgentExecutor(
            runner=create_runner,
            config=A2aAgentExecutorConfig(stream=self.stream),
        )

        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        request_handler = DefaultRequestHandlerV2(
            agent_executor=agent_executor,
            task_store=task_store,
            agent_card=self.agent_card,
            request_context_builder=request_context_builder,
        )

        faulthandler.enable()

        lifespan_manager = LifespanManager()
        lifespan_manager.add(self._lifespan)
        lifespan_manager.add(self._grpc_lifespan(request_handler))
        if not local:
            lifespan_manager.add(token_service.lifespan())
            lifespan_manager.add(controller_client.lifespan())

        app = FastAPI(lifespan=lifespan_manager)
        app.add_middleware(
            A2ARequestSizeLimitMiddleware,
            max_content_length=get_a2a_max_content_length(),
        )

        # Health check/readiness probe
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        add_a2a_routes_to_fastapi(
            app,
            agent_card_routes=create_agent_card_routes(self.agent_card),
            jsonrpc_routes=create_jsonrpc_routes(request_handler, rpc_url="/"),
        )

        return app

    def _grpc_lifespan(self, request_handler: DefaultRequestHandlerV2):
        @asynccontextmanager
        async def lifespan(app: FastAPI):
            server = grpc.aio.server()
            context_builder = KAgentGrpcServerCallContextBuilder()
            a2a_pb2_grpc.add_A2AServiceServicer_to_server(
                GrpcHandler(request_handler, context_builder=context_builder), server
            )

            health_service = health.aio.HealthServicer()
            service_name = a2a_pb2.DESCRIPTOR.services_by_name["A2AService"].full_name
            await health_service.set(service_name, health_pb2.HealthCheckResponse.SERVING)
            health_pb2_grpc.add_HealthServicer_to_server(health_service, server)

            port = server.add_insecure_port(self.a2a_grpc_address)
            if port == 0:
                raise RuntimeError(f"failed to bind A2A gRPC server to {self.a2a_grpc_address}")
            app.state.a2a_grpc_port = port
            await server.start()
            try:
                yield
            finally:
                await health_service.enter_graceful_shutdown()
                await server.stop(grace=5)

        return lifespan

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
