"""KAgent Dapr-Agents A2A Server Integration.

Provides the KAgentApp class that builds a FastAPI application
with A2A protocol support for Dapr-Agents DurableAgent.
"""

import faulthandler
import logging

import httpx
from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard
from dapr_agents import DurableAgent
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from kagent.core import KAgentConfig, configure_tracing
from kagent.core.a2a import KAgentRequestContextBuilder, KAgentTaskStore

from ._durable import DaprDurableAgentExecutor

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
    """Main application class for Dapr-Agents + KAgent integration.

    Builds a FastAPI application with A2A protocol support,
    using Dapr-Agents DurableAgent for execution and KAgent for state persistence.
    """

    def __init__(
        self,
        *,
        agent: DurableAgent,
        agent_card: AgentCard,
        config: KAgentConfig,
        tracing: bool = True,
    ):
        self._agent = agent
        self.agent_card = AgentCard.model_validate(agent_card)
        self.config = config
        self._enable_tracing = tracing

    def build(self) -> FastAPI:
        """Build the FastAPI application with A2A integration.

        Returns:
            Configured FastAPI application ready for deployment.
        """
        http_client = httpx.AsyncClient(base_url=self.config.url)

        agent_executor = DaprDurableAgentExecutor(
            durable_agent=self._agent,
            app_name=self.config.app_name,
        )

        task_store = KAgentTaskStore(http_client)
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)

        request_handler = DefaultRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            request_context_builder=request_context_builder,
        )

        a2a_app = A2AStarletteApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
        )

        faulthandler.enable()

        app = FastAPI(
            title=f"KAgent Dapr-Agents: {self.config.app_name}",
            description=f"Dapr-Agents agent with KAgent integration: {self.agent_card.description}",
            version=self.agent_card.version,
        )

        if self._enable_tracing:
            try:
                configure_tracing(app)
                logger.info("Tracing configured for KAgent Dapr-Agents app")
            except Exception:
                logger.exception("Failed to configure tracing")

        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)

        a2a_app.add_routes_to_app(app)

        return app
