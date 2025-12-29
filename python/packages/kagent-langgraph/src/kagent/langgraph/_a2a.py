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
from kagent.core.a2a import KAgentRequestContextBuilder, KAgentTaskStore
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


def _patch_a2a_payload_limit(max_body_size: int):
    """Attempt to patch a2a-python library's hardcoded payload size limit."""
    try:
        # Try different import paths for jsonrpc_app module
        jsonrpc_app = None
        import_paths = [
            "a2a.server.apps.jsonrpc.jsonrpc_app",
            "a2a.server.apps.jsonrpc_app",
        ]
        for path in import_paths:
            try:
                jsonrpc_app = __import__(path, fromlist=[""])
                break
            except ImportError:
                continue

        if jsonrpc_app is None:
            logger.debug("Could not find a2a-python jsonrpc_app module to patch")
            return

        # Check if MAX_PAYLOAD_SIZE or similar constant exists
        if hasattr(jsonrpc_app, "MAX_PAYLOAD_SIZE"):
            jsonrpc_app.MAX_PAYLOAD_SIZE = max_body_size
            logger.info(f"Patched a2a-python MAX_PAYLOAD_SIZE to {max_body_size} bytes")
        # Also check for _MAX_PAYLOAD_SIZE or other variants
        elif hasattr(jsonrpc_app, "_MAX_PAYLOAD_SIZE"):
            jsonrpc_app._MAX_PAYLOAD_SIZE = max_body_size
            logger.info(f"Patched a2a-python _MAX_PAYLOAD_SIZE to {max_body_size} bytes")
        else:
            logger.debug("Could not find MAX_PAYLOAD_SIZE constant in a2a-python jsonrpc_app")
    except (ImportError, AttributeError) as e:
        # If patching fails, log a debug message but continue
        logger.debug(f"Could not patch a2a-python payload limit: {e}")


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
        max_payload_size: int | None = None,
    ):
        """Initialize the KAgent application.

        Args:
            graph: Pre-compiled LangGraph
            agent_card: Agent card configuration for A2A protocol
            config: KAgent configuration
            executor_config: Optional executor configuration
            tracing: Enable OpenTelemetry tracing/logging via kagent.core.tracing
            max_payload_size: Maximum payload size in bytes for A2A requests

        """
        self._graph = graph
        self.agent_card = AgentCard.model_validate(agent_card)
        self.config = config

        self.executor_config = executor_config or LangGraphAgentExecutorConfig()
        self._enable_tracing = tracing
        self.max_payload_size = max_payload_size

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
        a2a_app = A2AStarletteApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
        )

        # Enable fault handler for debugging
        faulthandler.enable()

        # Patch a2a-python's payload size limit if specified
        if self.max_payload_size is not None:
            _patch_a2a_payload_limit(self.max_payload_size)

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
