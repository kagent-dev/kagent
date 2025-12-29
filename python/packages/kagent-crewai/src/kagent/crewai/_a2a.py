import faulthandler
import logging
import os
from typing import Union

import httpx
from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from opentelemetry.instrumentation.crewai import CrewAIInstrumentor

from crewai import Crew, Flow
from kagent.core import KAgentConfig, configure_tracing
from kagent.core.a2a import KAgentRequestContextBuilder, KAgentTaskStore

from ._executor import CrewAIAgentExecutor, CrewAIAgentExecutorConfig

logger = logging.getLogger(__name__)


def def_health_check(request: Request) -> PlainTextResponse:
    return PlainTextResponse("OK")


def thread_dump(request: Request) -> PlainTextResponse:
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
    def __init__(
        self,
        *,
        crew: Union[Crew, Flow],
        agent_card: AgentCard,
        config: KAgentConfig = KAgentConfig(),
        executor_config: CrewAIAgentExecutorConfig | None = None,
        tracing: bool = True,
        max_payload_size: int | None = None,
    ):
        self._crew = crew
        self.agent_card = AgentCard.model_validate(agent_card)
        self.config = config
        self.executor_config = executor_config or CrewAIAgentExecutorConfig()
        self.tracing = tracing
        self.max_payload_size = max_payload_size

    def build(self) -> FastAPI:
        http_client = httpx.AsyncClient(base_url=self.config.url)

        agent_executor = CrewAIAgentExecutor(
            crew=self._crew,
            app_name=self.config.app_name,
            config=self.executor_config,
            http_client=http_client,
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

        # Patch a2a-python's payload size limit if specified
        if self.max_payload_size is not None:
            _patch_a2a_payload_limit(self.max_payload_size)

        app = FastAPI(
            title=f"KAgent CrewAI: {self.config.app_name}",
            description=f"CrewAI agent with KAgent integration: {self.agent_card.description}",
            version=self.agent_card.version,
        )

        if self.tracing:
            configure_tracing(app)
            # Setup crewAI instrumentor separately as core configure does not include it
            tracing_enabled = os.getenv("OTEL_TRACING_ENABLED", "false").lower() == "true"
            if tracing_enabled:
                CrewAIInstrumentor().instrument()

        app.add_route("/health", methods=["GET"], route=def_health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        a2a_app.add_routes_to_app(app)

        return app
