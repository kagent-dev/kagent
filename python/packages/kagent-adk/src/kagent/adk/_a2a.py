#! /usr/bin/env python3
import faulthandler
import logging
import os
from typing import Callable, List

import httpx
from a2a.server.apps import A2AFastAPIApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.tasks import InMemoryTaskStore
from a2a.types import AgentCard
from agentsts.adk import ADKSTSIntegration, ADKTokenPropagationPlugin
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.adk.agents import BaseAgent
from google.adk.apps import App
from google.adk.artifacts import InMemoryArtifactService
from google.adk.plugins import BasePlugin
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.genai import types

from kagent.core.a2a import KAgentRequestContextBuilder, KAgentTaskStore

from ._agent_executor import A2aAgentExecutor
from ._session_service import KAgentSessionService
from ._token import KAgentTokenService


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
        plugins: List[BasePlugin] = None,
    ):
        self.root_agent = root_agent
        self.kagent_url = kagent_url
        self.app_name = app_name
        self.agent_card = agent_card
        self.plugins = plugins if plugins is not None else []

    def build(self) -> FastAPI:
        token_service = KAgentTokenService(self.app_name)
        http_client = httpx.AsyncClient(  # TODO: add user  and agent headers
            base_url=kagent_url_override or self.kagent_url, event_hooks=token_service.event_hooks()
        )
        session_service = KAgentSessionService(http_client)

        if sts_well_known_uri:
            sts_integration = ADKSTSIntegration(sts_well_known_uri)
            self.plugins.append(ADKTokenPropagationPlugin(sts_integration))

        adk_app = App(name=self.app_name, root_agent=self.root_agent, plugins=self.plugins)

        def create_runner() -> Runner:
            return Runner(
                app=adk_app,
                session_service=session_service,
                artifact_service=InMemoryArtifactService(),
            )

        agent_executor = A2aAgentExecutor(
            runner=create_runner,
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
        app = FastAPI(lifespan=token_service.lifespan())

        # Health check/readiness probe
        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        a2a_app.add_routes_to_app(app)

        return app

    def build_local(self) -> FastAPI:
        session_service = InMemorySessionService()

        def create_runner() -> Runner:
            return Runner(
                agent=self.root_agent,
                app_name=self.app_name,
                session_service=session_service,
                artifact_service=InMemoryArtifactService(),
            )

        agent_executor = A2aAgentExecutor(
            runner=create_runner,
        )

        task_store = InMemoryTaskStore()
        request_context_builder = KAgentRequestContextBuilder(task_store=task_store)
        request_handler = DefaultRequestHandler(
            agent_executor=agent_executor,
            task_store=task_store,
            request_context_builder=request_context_builder,
        )

        a2a_app = A2AFastAPIApplication(
            agent_card=self.agent_card,
            http_handler=request_handler,
        )

        faulthandler.enable()
        app = FastAPI()

        app.add_route("/health", methods=["GET"], route=health_check)
        app.add_route("/thread_dump", methods=["GET"], route=thread_dump)
        a2a_app.add_routes_to_app(app)

        return app

    async def test(self, task: str):
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
            artifact_service=InMemoryArtifactService(),
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
