#! /usr/bin/env python3

import logging
import os
from contextlib import asynccontextmanager

import httpx
from a2a.auth.user import User
from a2a.server.agent_execution import RequestContext, SimpleRequestContextBuilder
from a2a.server.apps import A2AStarletteApplication
from a2a.server.context import ServerCallContext
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import TaskStore
from a2a.types import AgentCard, MessageSendParams, Task
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.adk.a2a.executor.a2a_agent_executor import A2aAgentExecutor
from google.adk.agents import BaseAgent
from google.adk.runners import Runner

from kagent import KAgentSessionService, KAgentTaskStore

# --- Constants ---
USER_ID = "admin@kagent.dev"

# --- Configure Logging ---
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class KAgentUser(User):
    def __init__(self, user_id: str):
        self.user_id = user_id

    @property
    def is_authenticated(self) -> bool:
        return False

    @property
    def user_name(self) -> str:
        return self.user_id


class KAgentRequestContextBuilder(SimpleRequestContextBuilder):
    """
    A request context builder that will be used to hack in the user_id for now.
    """

    def __init__(self, user_id: str, task_store: TaskStore):
        super().__init__(task_store=task_store)
        self.user_id = user_id

    async def build(
        self,
        params: MessageSendParams | None = None,
        task_id: str | None = None,
        context_id: str | None = None,
        task: Task | None = None,
        context: ServerCallContext | None = None,
    ) -> RequestContext:
        if not context:
            context = ServerCallContext(user=KAgentUser(user_id=self.user_id))
        else:
            context.user = KAgentUser(user_id=self.user_id)
        request_context = await super().build(params, task_id, context_id, task, context)
        return request_context


def health_check(request: Request) -> PlainTextResponse:
    return PlainTextResponse("OK")


kagent_url_override = os.getenv("KAGENT_URL")


def build_app(root_agent: BaseAgent, kagent_url: str, app_name: str, agent_card: AgentCard) -> FastAPI:
    http_client = httpx.AsyncClient(base_url=kagent_url_override or kagent_url)
    session_service = KAgentSessionService(http_client)
    runner = Runner(
        agent=root_agent,
        app_name=app_name,
        session_service=session_service,
    )

    agent_executor = A2aAgentExecutor(
        runner=runner,
    )

    kagent_task_store = KAgentTaskStore(http_client)

    request_context_builder = KAgentRequestContextBuilder(user_id=USER_ID, task_store=kagent_task_store)
    request_handler = DefaultRequestHandler(
        agent_executor=agent_executor,
        task_store=kagent_task_store,
        request_context_builder=request_context_builder,
    )

    a2a_app = A2AStarletteApplication(
        agent_card=agent_card,
        http_handler=request_handler,
    )

    @asynccontextmanager
    async def agent_lifespan(app: FastAPI):
        yield
        await runner.close()

    app = FastAPI(lifespan=agent_lifespan)

    # Health check/readiness probe
    app.add_route("/health", methods=["GET"], route=health_check)

    a2a_app.add_routes_to_app(app)

    return app
