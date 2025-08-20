"""KAgent LangGraph A2A Server Integration.

This module provides the main KAgentApp class that builds a FastAPI application
with A2A protocol support for LangGraph workflows.
"""

import faulthandler
import logging
import os
from typing import Any, Awaitable, Callable, Dict, Union

import httpx
from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from kagent import KAgentRequestContextBuilder, KAgentTaskStore
from langgraph.graph.state import CompiledStateGraph, StateGraph

from ._checkpointer import KAgentCheckpointer
from ._executor import LangGraphAgentExecutor, LangGraphAgentExecutorConfig

# --- Constants ---
DEFAULT_USER_ID = "admin@kagent.dev"

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
        graph: Union[CompiledStateGraph, Callable[..., Union[CompiledStateGraph, Awaitable[CompiledStateGraph]]]]
        | None = None,
        graph_builder: StateGraph | None = None,
        agent_card: Dict[str, Any],
        kagent_url: str,
        app_name: str,
        user_id: str = DEFAULT_USER_ID,
        executor_config: LangGraphAgentExecutorConfig | None = None,
    ):
        """Initialize the KAgent application.

        Args:
            graph: Pre-compiled LangGraph or factory function (optional)
            graph_builder: StateGraph builder to compile with KAgent checkpointer (optional)
            agent_card: Agent card configuration for A2A protocol
            kagent_url: Base URL of the KAgent server
            app_name: Application name for session management
            user_id: Default user ID for requests
            executor_config: Optional executor configuration

        Note:
            Either `graph` or `graph_builder` must be provided, but not both.
        """
        if graph is None and graph_builder is None:
            raise ValueError("Either 'graph' or 'graph_builder' must be provided")
        if graph is not None and graph_builder is not None:
            raise ValueError("Only one of 'graph' or 'graph_builder' can be provided")

        self._graph = graph
        self._graph_builder = graph_builder
        self.agent_card = AgentCard.model_validate(agent_card)
        self.kagent_url = kagent_url
        self.app_name = app_name
        self.user_id = user_id
        self.executor_config = executor_config or LangGraphAgentExecutorConfig()

    def _create_graph_factory(self, http_client: httpx.AsyncClient) -> Callable[[], CompiledStateGraph]:
        """Create a factory function that returns a compiled graph with checkpointer."""
        if self._graph is not None:
            # Pre-compiled graph provided
            if callable(self._graph):
                return self._graph
            else:
                return lambda: self._graph

        # Graph builder provided - compile with KAgent checkpointer
        def create_compiled_graph() -> CompiledStateGraph:
            checkpointer = KAgentCheckpointer(http_client, self.app_name)
            return self._graph_builder.compile(checkpointer=checkpointer)

        return create_compiled_graph

    def build(self) -> FastAPI:
        """Build the FastAPI application with A2A integration.

        Returns:
            Configured FastAPI application ready for deployment
        """
        # Override URL from environment if provided
        kagent_url_override = os.getenv("KAGENT_URL")
        base_url = kagent_url_override or self.kagent_url

        # Create HTTP client for KAgent API
        http_client = httpx.AsyncClient(base_url=base_url)

        # Create graph factory
        graph_factory = self._create_graph_factory(http_client)

        # Create agent executor
        agent_executor = LangGraphAgentExecutor(
            graph=graph_factory,
            app_name=self.app_name,
            config=self.executor_config,
        )

        # Create task store
        task_store = KAgentTaskStore(http_client)

        # Create request context builder
        request_context_builder = KAgentRequestContextBuilder(user_id=self.user_id, task_store=task_store)

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

        # Create FastAPI application
        app = FastAPI(
            title=f"KAgent LangGraph: {self.app_name}",
            description=f"LangGraph agent with KAgent integration: {self.agent_card.description}",
            version=self.agent_card.version,
        )

        # Add health check and debugging routes
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)

        # Add A2A routes
        a2a_app.add_routes_to_app(app)

        return app

    async def test(self, task: str, session_id: str = "test-session") -> None:
        """Test the agent with a simple task (for development/debugging).

        Args:
            task: The task/question to send to the agent
            session_id: Session ID to use for the test
        """
        logger.info(f"Testing agent with task: {task}")

        # Create HTTP client
        http_client = httpx.AsyncClient(base_url=self.kagent_url)

        try:
            # Create and compile graph
            graph_factory = self._create_graph_factory(http_client)
            graph = graph_factory()

            # Prepare input
            from langchain_core.messages import HumanMessage

            input_data = {"messages": [HumanMessage(content=task)]}

            # Create config
            config = {
                "configurable": {
                    "thread_id": session_id,
                    "user_id": self.user_id,
                    "app_name": self.app_name,
                }
            }

            # Run the graph
            logger.info("Running graph...")
            async for event in graph.astream_events(input_data, config, version="v2"):
                event_type = event.get("event", "")
                if event_type in ["on_chat_model_stream", "on_chain_end"]:
                    logger.info(f"Event: {event_type} - {event.get('data', {})}")

            logger.info("Test completed successfully")

        except Exception as e:
            logger.error(f"Test failed: {e}", exc_info=True)
            raise
        finally:
            await http_client.aclose()
