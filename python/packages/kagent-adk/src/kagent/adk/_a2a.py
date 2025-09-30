#! /usr/bin/env python3
import faulthandler
import logging
import os
from contextlib import asynccontextmanager
from typing import Callable, Optional

import httpx
from a2a.server.apps import A2AFastAPIApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.adk.agents import BaseAgent
from google.adk.apps import App
from google.adk.plugins.base_plugin import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.genai import types

from kagent.core.a2a import KAgentRequestContextBuilder, KAgentTaskStore

from ._agent_executor import A2aAgentExecutor
from ._service_account_service import KAgentServiceAccountService
from ._session_service import KAgentSessionService
from ._sts_token_service import KAgentSTSTokenService
from ._token import KAgentTokenService
from .token_plugin import TokenPropagationPlugin
from .wrapped_session_service import WrappedSessionService


def configure_logging() -> None:
    """Configure logging based on LOG_LEVEL environment variable."""
    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    numeric_level = getattr(logging, log_level, logging.INFO)
    logging.basicConfig(
        level=numeric_level,
    )
    logging.info(f"Logging configured with level: {log_level}")


configure_logging()
logger = logging.getLogger(__name__)


def health_check(request: Request) -> PlainTextResponse:
    return PlainTextResponse("OK")


def thread_dump(request: Request) -> PlainTextResponse:
    import io

    buf = io.StringIO()
    faulthandler.dump_traceback(file=buf)
    buf.seek(0)
    return PlainTextResponse(buf.read())


kagent_url_override = os.getenv("KAGENT_URL")
sts_well_known_uri = os.getenv("STS_WELL_KNOWN_URI")


class KAgentApp:
    def __init__(
        self,
        root_agent: BaseAgent,
        agent_card: AgentCard,
        kagent_url: str,
        app_name: str,
    ):
        self.root_agent = root_agent
        self.kagent_url = kagent_url
        self.app_name = app_name
        self.agent_card = agent_card
        self.sts_service = None
        self.plugins = []

        # only store sts config if the well known uri is provided
        if sts_well_known_uri:
            self.sts_service = KAgentSTSTokenService(
                well_known_uri=sts_well_known_uri,
            )
            self.plugins.append(TokenPropagationPlugin())

    def build(self) -> FastAPI:
        token_service = KAgentTokenService(self.app_name)
        service_account_service = KAgentServiceAccountService(self.app_name)

        http_client = httpx.AsyncClient(
            base_url=kagent_url_override or self.kagent_url, event_hooks=token_service.event_hooks()
        )
        base_session_service = KAgentSessionService(http_client)

        # use wrapped session service if the sts service is available for token propagation
        # via the TokenPropagationPlugin
        if self.sts_service:
            session_service = WrappedSessionService(base_session_service, "")
        else:
            session_service = base_session_service

        adk_app = App(name=self.app_name, root_agent=self.root_agent, plugins=self.plugins)

        def create_runner() -> Runner:
            return Runner(
                app=adk_app,
                session_service=session_service,
            )

        agent_executor = A2aAgentExecutor(
            runner=create_runner,
            service_account_service=service_account_service,
            sts_service=self.sts_service,
        )

        kagent_task_store = KAgentTaskStore(http_client)

        request_context_builder = KAgentRequestContextBuilder(task_store=kagent_task_store)
        request_handler = DefaultRequestHandler(
            agent_executor=agent_executor,
            task_store=kagent_task_store,
            request_context_builder=request_context_builder,
        )

        a2a_app = A2AFastAPIApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
        )

        faulthandler.enable()

        # combine the lifespans of the token and service account services
        @asynccontextmanager
        async def combined_lifespan(app: FastAPI):
            async with token_service.lifespan()(app):
                if self.sts_service:
                    async with service_account_service.lifespan()(app):
                        yield
                else:
                    yield

        app = FastAPI(lifespan=combined_lifespan)

        # Health check/readiness probe
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        a2a_app.add_routes_to_app(app)

        return app

    async def test(self, task: str) -> str:
        session_service = InMemorySessionService()
        SESSION_ID = "12345"
        USER_ID = "admin"
        await session_service.create_session(
            app_name=self.app_name,
            session_id=SESSION_ID,
            user_id=USER_ID,
        )
        if isinstance(self.root_agent, Callable):
            agent_factory = self.root_agent
            root_agent = agent_factory()
        else:
            root_agent = self.root_agent

        runner = Runner(
            agent=root_agent,
            app_name=self.app_name,
            session_service=session_service,
        )

        logger.info(f"\n>>> User Query: {task}")

        # Prepare the user's message in ADK format
        content = types.Content(role="user", parts=[types.Part(text=task)])
        # Key Concept: run_async executes the agent logic and yields Events.
        # We iterate through events to find the final answer.
        async for event in runner.run_async(
            user_id=USER_ID,
            session_id=SESSION_ID,
            new_message=content,
        ):
            # You can uncomment the line below to see *all* events during execution
            # print(f"  [Event] Author: {event.author}, Type: {type(event).__name__}, Final: {event.is_final_response()}, Content: {event.content}")

            # Key Concept: is_final_response() marks the concluding message for the turn.
            jsn = event.model_dump_json()
            logger.info(f"  [Event] {jsn}")
