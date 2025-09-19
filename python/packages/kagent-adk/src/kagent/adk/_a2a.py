#! /usr/bin/env python3
import faulthandler
import logging
import os
from typing import Callable, Optional

import httpx
from a2a.server.apps import A2AFastAPIApplication
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.types import AgentCard
from fastapi import FastAPI, Request
from fastapi.responses import PlainTextResponse
from google.adk.agents import BaseAgent
from google.adk.runners import Runner
from google.adk.sessions import InMemorySessionService
from google.adk.apps.app import App
from google.adk.auth.auth_credential import AuthCredential
from google.adk.tools.mcp_tool import MCPTool
from google.adk.tools.tool_context import ToolContext
from google.adk.tools.base_tool import BaseTool
from google.adk.plugins.base_plugin import BasePlugin
from google.genai import types

from kagent.core.a2a import KAgentRequestContextBuilder, KAgentTaskStore

from ._agent_executor import A2aAgentExecutor
from ._session_service import KAgentSessionService
from ._token import KAgentTokenService

# --- Configure Logging ---
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

class TokenPropagationTool(McpTool):
    def __init__(self, other : McpTool):
        super().__init__(mcp_tool=other._mcp_tool, mcp_session_manager=other._mcp_session_manager)

    async def _get_headers(
        self, tool_context: ToolContext, credential: AuthCredential
    ) -> Optional[dict[str, str]]:
        return super()._get_headers(tool_context, credential)

class TokenPropagationPlugin(BasePlugin):
    def __init__(self):
        super().__init__()


    async def before_tool_callback(
        self,
        *,
        tool: BaseTool,
        tool_args: dict[str, Any],
        tool_context: ToolContext,
    ) -> Optional[dict]:
        # if tool is mcp tool, propagate headers
        if isinstance(tool, McpTool):
            return await TokenPropagationTool(tool).run_async(args=tool_args, tool_context=tool_context)
        return None
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
        self.plugins = []

    def build(self) -> FastAPI:
        token_service = KAgentTokenService(self.app_name)
        http_client = httpx.AsyncClient(  # TODO: add user  and agent headers
            base_url=kagent_url_override or self.kagent_url, event_hooks=token_service.event_hooks()
        )
        session_service = KAgentSessionService(http_client)

        app = App(name=self.app_name, root_agent=self.root_agent, plugins = self.plugins)

        def create_runner() -> Runner:
            return Runner(
                app=app,
                session_service=session_service,
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
