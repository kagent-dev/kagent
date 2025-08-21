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

from kagent.core import KAgentRequestContextBuilder, KAgentTaskStore
from langgraph.graph.state import CompiledStateGraph, RunnableConfig


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
        graph: CompiledStateGraph,
        agent_card: AgentCard,
        kagent_url: str,
        app_name: str,
        user_id: str = DEFAULT_USER_ID,
        executor_config: LangGraphAgentExecutorConfig | None = None,
    ):
        """Initialize the KAgent application.

        Args:
            graph: Pre-compiled LangGraph
            agent_card: Agent card configuration for A2A protocol
            kagent_url: Base URL of the KAgent server
            app_name: Application name for session management
            user_id: Default user ID for requests
            executor_config: Optional executor configuration

        """
        self._graph = graph
        self.agent_card = AgentCard.model_validate(agent_card)
        self.kagent_url = kagent_url
        self.app_name = app_name
        self.user_id = user_id
        self.executor_config = executor_config or LangGraphAgentExecutorConfig()

    def build(self) -> FastAPI:
        """Build the FastAPI application with A2A integration.

        Returns:
            Configured FastAPI application ready for deployment
        """

        # Create HTTP client for KAgent API
        http_client = httpx.AsyncClient(base_url=self.kagent_url)

        # Create agent executor
        agent_executor = LangGraphAgentExecutor(
            graph=self._graph,
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
