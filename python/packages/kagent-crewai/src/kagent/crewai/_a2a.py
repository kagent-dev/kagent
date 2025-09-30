import faulthandler
import logging
from typing import Union

import httpx
from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse

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


class KAgentApp:
    def __init__(
        self,
        *,
        crew: Union[Crew, Flow],
        agent_card: AgentCard,
        config: KAgentConfig = KAgentConfig(),
        executor_config: CrewAIAgentExecutorConfig | None = None,
        tracing: bool = True,
    ):
        self._crew = crew
        self.agent_card = AgentCard.model_validate(agent_card)
        self.config = config
        self.executor_config = executor_config or CrewAIAgentExecutorConfig()
        self.tracing = tracing

    def build(self) -> FastAPI:
        http_client = httpx.AsyncClient(base_url=self.config.url)

        agent_executor = CrewAIAgentExecutor(
            crew=self._crew,
            app_name=self.config.app_name,
            config=self.executor_config,
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
            title=f"KAgent CrewAI: {self.config.app_name}",
            description=f"CrewAI agent with KAgent integration: {self.agent_card.description}",
            version=self.agent_card.version,
        )

        if self.tracing:
            configure_tracing(app)

        app.add_route("/health", methods=["GET"], route=def_health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        a2a_app.add_routes_to_app(app)

        return app
