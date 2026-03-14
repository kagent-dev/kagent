from __future__ import annotations

from typing import Any
from unittest.mock import MagicMock

import pytest
from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset
from google.genai import types

from kagent.adk._mcp_capability_tools import LoadKAgentMcpPromptTool, LoadKAgentMcpResourceTool
from kagent.adk._mcp_toolset import KAgentMcpToolset
from kagent.adk.types import AgentConfig, HttpMcpServerConfig, OpenAI


class FakeLlmRequest:
    def __init__(self, responses: list[tuple[str, dict[str, Any]]]):
        self.contents = [
            types.Content(
                role="user",
                parts=[
                    types.Part(
                        function_response=types.FunctionResponse(
                            name=response_name,
                            response=response_payload,
                        )
                    )
                    for response_name, response_payload in responses
                ],
            )
        ]
        self.instructions: list[str] = []
        self.appended_tools: list[Any] = []

    def append_instructions(self, instructions: list[str]) -> None:
        self.instructions.extend(instructions)

    def append_tools(self, tools: list[Any]) -> None:
        self.appended_tools.extend(tools)


class StubPromptToolset:
    async def list_prompt_info(self, readonly_context: Any = None) -> list[dict[str, Any]]:
        return [
            {
                "name": "incident_triage",
                "description": "Guide incident triage",
                "arguments": [{"name": "ticket_id", "description": "Incident identifier", "required": True}],
            }
        ]

    async def get_prompt(
        self,
        name: str,
        arguments: dict[str, str] | None = None,
        readonly_context: Any = None,
    ) -> dict[str, Any]:
        assert name == "incident_triage"
        assert arguments == {"ticket_id": "INC-42"}
        return {
            "messages": [
                {
                    "role": "assistant",
                    "content": {
                        "type": "text",
                        "text": "Investigate the controller logs first.",
                    },
                }
            ]
        }


class StubResourceToolset:
    async def list_resources(self, readonly_context: Any = None) -> list[str]:
        return ["cluster_runbook"]

    async def read_resource(self, name: str, readonly_context: Any = None) -> list[dict[str, Any]]:
        assert name == "cluster_runbook"
        return [{"text": "Restart the controller deployment if reconciliation is stuck."}]


class StubCombinedToolset(StubPromptToolset, StubResourceToolset):
    """Supports both prompts and resources for combined-call tests."""

    pass


def _make_agent_config(url: str) -> AgentConfig:
    return AgentConfig(
        model=OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake"),
        description="Test agent",
        instruction="You are a test agent",
        http_tools=[
            HttpMcpServerConfig(
                params=StreamableHTTPConnectionParams(url=url, headers=None),
                tools=["test-tool"],
            )
        ],
    )


@pytest.mark.asyncio
async def test_kagent_mcp_toolset_adds_prompt_and_resource_helpers(monkeypatch):
    async def _base_get_tools(self, readonly_context=None):
        return []

    async def _list_resources(self, readonly_context=None):
        return ["cluster_runbook"]

    async def _list_prompt_info(self, readonly_context=None):
        return [{"name": "incident_triage", "arguments": []}]

    monkeypatch.setattr(McpToolset, "get_tools", _base_get_tools)
    monkeypatch.setattr(KAgentMcpToolset, "list_resources", _list_resources)
    monkeypatch.setattr(KAgentMcpToolset, "list_prompt_info", _list_prompt_info)

    agent = _make_agent_config("http://tools.kagent:8080/mcp").to_agent("test_agent")
    mcp_toolset = next(tool for tool in agent.tools if isinstance(tool, KAgentMcpToolset))

    helper_tools = await mcp_toolset.get_tools()
    helper_names = {tool.name for tool in helper_tools}

    assert mcp_toolset.resource_tool_name in helper_names
    assert mcp_toolset.prompt_tool_name in helper_names


@pytest.mark.asyncio
async def test_kagent_mcp_toolset_skips_helpers_when_capabilities_are_missing(monkeypatch):
    async def _base_get_tools(self, readonly_context=None):
        return []

    async def _list_resources(self, readonly_context=None):
        return []

    async def _list_prompt_info(self, readonly_context=None):
        return []

    monkeypatch.setattr(McpToolset, "get_tools", _base_get_tools)
    monkeypatch.setattr(KAgentMcpToolset, "list_resources", _list_resources)
    monkeypatch.setattr(KAgentMcpToolset, "list_prompt_info", _list_prompt_info)

    agent = _make_agent_config("http://tools.kagent:8080/mcp").to_agent("test_agent")
    mcp_toolset = next(tool for tool in agent.tools if isinstance(tool, KAgentMcpToolset))

    assert await mcp_toolset.get_tools() == []


def test_kagent_mcp_toolset_generates_unique_helper_names_per_server():
    first_agent = _make_agent_config("https://gateway.example/mcp/team-a").to_agent("first_agent")
    second_agent = _make_agent_config("https://gateway.example/mcp/team-b").to_agent("second_agent")

    first_toolset = next(tool for tool in first_agent.tools if isinstance(tool, KAgentMcpToolset))
    second_toolset = next(tool for tool in second_agent.tools if isinstance(tool, KAgentMcpToolset))

    assert first_toolset.resource_tool_name != second_toolset.resource_tool_name
    assert first_toolset.prompt_tool_name != second_toolset.prompt_tool_name


@pytest.mark.asyncio
async def test_prompt_loader_adds_prompt_catalog_and_contents():
    tool = LoadKAgentMcpPromptTool(
        mcp_toolset=StubPromptToolset(),
        name="load_mcp_prompt_incident",
        server_label="incident-mcp",
    )
    llm_request = FakeLlmRequest(
        responses=[
            ("other_tool", {"status": "ignored"}),
            (tool.name, {"prompt_name": "incident_triage", "arguments": {"ticket_id": "INC-42"}}),
        ],
    )

    await tool.process_llm_request(tool_context=MagicMock(), llm_request=llm_request)

    assert llm_request.appended_tools == [tool]
    assert any("incident_triage" in instruction for instruction in llm_request.instructions)
    assert llm_request.contents[1].role == "model"
    assert any(
        part.text and "Investigate the controller logs first." in part.text
        for content in llm_request.contents[1:]
        for part in content.parts
        if getattr(part, "text", None)
    )


@pytest.mark.asyncio
async def test_resource_loader_coerces_string_resource_names():
    tool = LoadKAgentMcpResourceTool(
        mcp_toolset=StubResourceToolset(),
        name="load_mcp_resource_cluster",
        server_label="cluster-mcp",
    )

    result = await tool.run_async(args={"resource_names": "cluster_runbook"}, tool_context=MagicMock())
    assert result["resource_names"] == ["cluster_runbook"]

    result = await tool.run_async(args={"resource_names": 12345}, tool_context=MagicMock())
    assert result["resource_names"] == []

    result = await tool.run_async(args={}, tool_context=MagicMock())
    assert result["resource_names"] == []


@pytest.mark.asyncio
async def test_resource_loader_adds_resource_catalog_and_contents():
    tool = LoadKAgentMcpResourceTool(
        mcp_toolset=StubResourceToolset(),
        name="load_mcp_resource_cluster",
        server_label="cluster-mcp",
    )
    llm_request = FakeLlmRequest(
        responses=[
            ("other_tool", {"status": "ignored"}),
            (tool.name, {"resource_names": ["cluster_runbook"]}),
        ],
    )

    await tool.process_llm_request(tool_context=MagicMock(), llm_request=llm_request)

    assert llm_request.appended_tools == [tool]
    assert any("cluster_runbook" in instruction for instruction in llm_request.instructions)
    assert any(
        part.text and "Restart the controller deployment" in part.text
        for content in llm_request.contents[1:]
        for part in content.parts
        if getattr(part, "text", None)
    )


@pytest.mark.asyncio
async def test_combined_resource_and_prompt_helpers_in_same_turn():
    """Both helpers called in one turn — the second must still find its function response."""
    toolset = StubCombinedToolset()

    resource_tool = LoadKAgentMcpResourceTool(
        mcp_toolset=toolset,
        name="load_mcp_resource_cluster",
        server_label="cluster-mcp",
    )
    prompt_tool = LoadKAgentMcpPromptTool(
        mcp_toolset=toolset,
        name="load_mcp_prompt_incident",
        server_label="cluster-mcp",
    )

    llm_request = FakeLlmRequest(
        responses=[
            (resource_tool.name, {"resource_names": ["cluster_runbook"]}),
            (prompt_tool.name, {"prompt_name": "incident_triage", "arguments": {"ticket_id": "INC-42"}}),
        ],
    )

    # Resource helper runs first and appends content to llm_request.contents
    await resource_tool.process_llm_request(tool_context=MagicMock(), llm_request=llm_request)
    # Prompt helper runs second — must still find its function response
    await prompt_tool.process_llm_request(tool_context=MagicMock(), llm_request=llm_request)

    # Verify resource content was loaded
    assert any(
        part.text and "Restart the controller deployment" in part.text
        for content in llm_request.contents[1:]
        for part in content.parts
        if getattr(part, "text", None)
    )

    # Verify prompt content was loaded (this would fail before the fix)
    assert any(
        part.text and "Investigate the controller logs first." in part.text
        for content in llm_request.contents[1:]
        for part in content.parts
        if getattr(part, "text", None)
    )
