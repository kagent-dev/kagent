"""KAgent LangGraph A2A Server Integration.

This module provides the main KAgentApp class that builds a FastAPI application
with A2A protocol support for LangGraph workflows.
"""

import faulthandler
import logging

import httpx
from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse

from kagent.core import KAgentConfig, configure_tracing
from kagent.core.a2a import (
    KAgentRequestContextBuilder,
    KAgentTaskStore,
    get_a2a_max_content_length,
)
from langgraph.graph.state import CompiledStateGraph

from ._executor import LangGraphAgentExecutor, LangGraphAgentExecutorConfig

# --- Configure Logging ---
logger = logging.getLogger(__name__)


def health_check(request: Request) -> PlainTextResponse:
    """Health check endpoint."""
    return PlainTextResponse("OK")


def thread_dump(request: Request) -> PlainTextResponse:
    """Thread dump endpoint for debugging."""
    import io

    buf = io.StringIO()
    faulthandler.dump_traceback(file=buf)
    buf.seek(0)
    return PlainTextResponse(buf.read())


class KAgentApp:
    """Main application class for LangGraph + KAgent integration.

    This class builds a FastAPI application with A2A protocol support,
    using LangGraph for agent execution and KAgent for state persistence.
    """

    def __init__(
        self,
        *,
        graph: CompiledStateGraph,
        agent_card: AgentCard,
        config: KAgentConfig,
        executor_config: LangGraphAgentExecutorConfig | None = None,
        tracing: bool = True,
    ):
        """Initialize the KAgent application.

        Args:
            graph: Pre-compiled LangGraph
            agent_card: Agent card configuration for A2A protocol
            config: KAgent configuration
            executor_config: Optional executor configuration
            tracing: Enable OpenTelemetry tracing/logging via kagent.core.tracing

        """
        self._graph = graph
        self.agent_card = AgentCard.model_validate(agent_card)
        self.config = config

        self.executor_config = executor_config or LangGraphAgentExecutorConfig()
        self._enable_tracing = tracing

    def build(self) -> FastAPI:
        """Build the FastAPI application with A2A integration.

        Returns:
            Configured FastAPI application ready for deployment
        """
        # Create HTTP client for KAgent API
        http_client = httpx.AsyncClient(base_url=self.config.url)

        # Create agent executor
        agent_executor = LangGraphAgentExecutor(
            graph=self._graph,
            app_name=self.config.app_name,
            config=self.executor_config,
        )

        # Create task store
        task_store = KAgentTaskStore(http_client)

        # Create request context builder
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)

        # Create request handler
        request_handler = DefaultRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            request_context_builder=request_context_builder,
        )

        # Create A2A application
        max_content_length = get_a2a_max_content_length()
        a2a_app = A2AStarletteApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
            max_content_length=max_content_length,
        )

        # Enable fault handler for debugging
        faulthandler.enable()

        # Create FastAPI application
        app = FastAPI(
            title=f"KAgent LangGraph: {self.config.app_name}",
            description=f"LangGraph agent with KAgent integration: {self.agent_card.description}",
            version=self.agent_card.version,
        )

        # Configure tracing/instrumentation if enabled
        if self._enable_tracing:
            try:
                configure_tracing(app)
                logger.info("Tracing configured for KAgent LangGraph app")
            except Exception:
                logger.exception("Failed to configure tracing")

        # Add health check and debugging routes
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)

        # Add A2A routes
        a2a_app.add_routes_to_app(app)

        return app
