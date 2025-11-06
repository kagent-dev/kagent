"""KAgent OpenAI Agents SDK Application.

This module provides the main KAgentApp class for building FastAPI applications
that integrate OpenAI Agents SDK with the A2A (Agent-to-Agent) protocol.
"""

from __future__ import annotations

import faulthandler
import logging
import os
from collections.abc import Callable
from typing import Path

import httpx
from a2a.server.apps import A2AFastAPIApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard
from agents.agent import Agent
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse

from kagent.core.a2a import KAgentRequestContextBuilder, KAgentTaskStore

from ._agent_executor import OpenAIAgentExecutor, OpenAIAgentExecutorConfig
from ._session_service import KAgentSessionFactory

# Configure logging
logger = logging.getLogger(__name__)


def configure_logging() -> None:
    """Configure logging based on LOG_LEVEL environment variable."""
    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    numeric_level = getattr(logging, log_level, logging.INFO)
    logging.basicConfig(level=numeric_level)
    logging.info(f"Logging configured with level: {log_level}")


configure_logging()


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


# Environment variables
kagent_url_override = os.getenv("KAGENT_URL")
sts_well_known_uri = os.getenv("STS_WELL_KNOWN_URI")


class KAgentApp:
    """FastAPI application builder for OpenAI Agents SDK with KAgent integration.

    This class builds a FastAPI application that:
    - Runs OpenAI Agents SDK agents
    - Integrates with KAgent's A2A protocol
    - Manages sessions via KAgent REST API
    - Streams agent execution events
    - Handles authentication and token propagation
    """

    def __init__(
        self,
        agent: Agent | Callable[[], Agent],
        agent_card: AgentCard,
        kagent_url: str,
        app_name: str,
        skills_directory: str | Path | None = None,
        config: OpenAIAgentExecutorConfig | None = None,
    ):
        """Initialize the KAgent application.

        Args:
            agent: OpenAI Agent instance or factory function
            agent_card: A2A agent card describing the agent's capabilities
            kagent_url: URL of the KAgent backend server
            app_name: Application name for identification
            skills_directory: Path to the skills directory for session initialization.
            config: Optional executor configuration
        """
        self.agent = agent
        self.kagent_url = kagent_url
        self.app_name = app_name
        self.agent_card = agent_card
        self.skills_directory = skills_directory
        self._config = config or OpenAIAgentExecutorConfig()

    def build(self) -> FastAPI:
        """Build a production FastAPI application with KAgent integration.

        This creates an application that:
        - Uses KAgentSessionFactory for session management
        - Connects to KAgent backend via REST API
        - Implements A2A protocol handlers
        - Includes health check endpoints

        Returns:
            Configured FastAPI application
        """

        # Create HTTP client with KAgent backend
        http_client = httpx.AsyncClient(
            base_url=kagent_url_override or self.kagent_url,
        )

        # Create session factory
        session_factory = KAgentSessionFactory(
            client=http_client,
            app_name=self.app_name,
        )

        # Create agent executor with session factory
        agent_executor = OpenAIAgentExecutor(
            agent=self.agent,
            app_name=self.app_name,
            skills_directory=self.skills_directory,
            session_factory=session_factory.create_session,
            config=self._config,
        )

        # Create KAgent task store
        kagent_task_store = KAgentTaskStore(http_client)

        # Create request context builder and handler
        request_context_builder = KAgentRequestContextBuilder(task_store=kagent_task_store)
        request_handler = DefaultRequestHandler(
            agent_executor=agent_executor,
            task_store=kagent_task_store,
            request_context_builder=request_context_builder,
        )

        # Create A2A FastAPI application
        a2a_app = A2AFastAPIApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
        )

        # Enable fault handler
        faulthandler.enable()

        # Create FastAPI app with lifespan
        app = FastAPI()

        # Add health check endpoints
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)

        # Add A2A routes
        a2a_app.add_routes_to_app(app)

        return app

    def build_local(self) -> FastAPI:
        """Build a local FastAPI application for testing without KAgent backend.

        This creates an application that:
        - Uses InMemoryTaskStore (no KAgent backend needed)
        - Runs agents without session persistence
        - Useful for local development and testing

        Returns:
            Configured FastAPI application for local use
        """
        # Create agent executor without session factory (no persistence)
        agent_executor = OpenAIAgentExecutor(
            agent=self.agent,
            app_name=self.app_name,
            skills_directory=self.skills_directory,
            session_factory=None,  # No session persistence in local mode
            config=self._config,
        )
        # Use in-memory task store
        task_store = InMemoryTaskStore()

        # Create request context builder and handler
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        request_handler = DefaultRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            request_context_builder=request_context_builder,
        )

        # Create A2A FastAPI application
        a2a_app = A2AFastAPIApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
        )

        # Enable fault handler
        faulthandler.enable()

        # Create FastAPI app
        app = FastAPI()

        # Add health check endpoints
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)

        # Add A2A routes
        a2a_app.add_routes_to_app(app)

        return app

    async def test(self, task: str) -> None:
        """Test the agent with a simple task.

        Args:
            task: The task/question to ask the agent
        """
        from agents.run import Runner

        # Resolve agent
        if callable(self.agent):
            agent = self.agent()
        else:
            agent = self.agent

        logger.info(f"\n>>> User Query: {task}")

        # Run the agent
        result = await Runner.run(agent, task)

        logger.info(f">>> Agent Response: {result.final_output}")
