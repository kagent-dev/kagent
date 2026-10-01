from unittest.mock import MagicMock

import pytest
from google.adk.agents import LlmAgent
from google.adk.agents.invocation_context import InvocationContext
from google.adk.flows.llm_flows import functions
from google.adk.plugins import BasePlugin
from google.adk.plugins.plugin_manager import PluginManager
from google.adk.sessions import InMemorySessionService
from google.genai import types

from kagent.adk import KAgentApp
from kagent.adk._tool_error_plugin import ToolErrorPlugin


@pytest.mark.asyncio
async def test_tool_error_is_returned_to_model():
    plugin = ToolErrorPlugin()

    result = await plugin.on_tool_error_callback(
        tool=MagicMock(),
        tool_args={},
        tool_context=MagicMock(),
        error=ValueError("Tool 'missing' not found"),
    )

    assert result == {"error": "Tool 'missing' not found"}


@pytest.mark.asyncio
async def test_unknown_tool_produces_function_response():
    session_service = InMemorySessionService()
    session = await session_service.create_session(app_name="test-app", user_id="user", session_id="session")
    agent = LlmAgent(name="test_agent")
    invocation_context = InvocationContext(
        session_service=session_service,
        invocation_id="invocation",
        agent=agent,
        session=session,
        plugin_manager=PluginManager([ToolErrorPlugin()]),
    )

    event = await functions._execute_single_function_call_async(
        invocation_context,
        types.FunctionCall(name="missing", args={}),
        {},
        agent,
    )

    assert event is not None
    response = event.content.parts[0].function_response.response
    assert response["error"].startswith("Tool 'missing' not found")


def test_tool_error_fallback_runs_after_custom_plugins():
    custom_plugin = BasePlugin(name="custom")

    app = KAgentApp(
        root_agent_factory=MagicMock(),
        agent_card=MagicMock(),
        kagent_api_url="http://unused",
        app_name="test-app",
        plugins=[custom_plugin],
    )

    assert app.plugins[0] is custom_plugin
    assert isinstance(app.plugins[1], ToolErrorPlugin)
