import faulthandler
import logging
from typing import Union

from a2a.server.routes import add_a2a_routes_to_fastapi, create_agent_card_routes, create_jsonrpc_routes
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.protobuf.json_format import ParseDict
from kagent.core import AsyncControllerClient, KAgentConfig, configure_tracing
from kagent.core.a2a import (
    A2ARequestSizeLimitMiddleware,
    KAgentRequestContextBuilder,
    get_a2a_max_content_length,
)
from kagent.core.a2a._task_store import KAgentRequestHandler, KAgentTaskStore
from kagent.core.tracing import signal_enabled
from opentelemetry.instrumentation.crewai import CrewAIInstrumentor

from crewai import Crew, Flow

from ._executor import CrewAIAgentExecutor, CrewAIAgentExecutorConfig

logger = logging.getLogger(__name__)


def def_health_check(request: Request) -> PlainTextResponse:
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
        *,
        crew: Union[Crew, Flow],
        agent_card: AgentCard | dict,
        config: KAgentConfig | None = None,
        executor_config: CrewAIAgentExecutorConfig | None = None,
        tracing: bool = True,
    ):
        self._crew = crew
        self.agent_card = ParseDict(agent_card, AgentCard()) if isinstance(agent_card, dict) else agent_card
        self.config = config or KAgentConfig()
        self.executor_config = executor_config or CrewAIAgentExecutorConfig()
        self.tracing = tracing

    def build(self) -> FastAPI:
        agent_executor = CrewAIAgentExecutor(
            crew=self._crew,
            app_name=self.config.app_name,
            config=self.executor_config,
        )

        controller = AsyncControllerClient(self.config.api_url)
        task_store = KAgentTaskStore(controller)
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        request_handler = KAgentRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            agent_card=self.agent_card,
            request_context_builder=request_context_builder,
        )

        faulthandler.enable()
        app = FastAPI(
            lifespan=controller.lifespan(),
            title=f"KAgent CrewAI: {self.config.app_name}",
            description=f"CrewAI agent with KAgent integration: {self.agent_card.description}",
            version=self.agent_card.version,
        )
        app.add_middleware(
            A2ARequestSizeLimitMiddleware,
            max_content_length=get_a2a_max_content_length(),
        )

        if self.tracing:
            configure_tracing(self.config.name, self.config.namespace, app)
            # Setup crewAI instrumentor separately as core configure does not include it
            if signal_enabled("TRACES"):
                CrewAIInstrumentor().instrument()

        app.add_route("/health", methods=["GET"], route=def_health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        add_a2a_routes_to_fastapi(
            app,
            agent_card_routes=create_agent_card_routes(self.agent_card),
            jsonrpc_routes=create_jsonrpc_routes(request_handler, rpc_url="/"),
        )

        return app
