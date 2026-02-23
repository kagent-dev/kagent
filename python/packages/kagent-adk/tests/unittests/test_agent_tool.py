# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from unittest.mock import AsyncMock, Mock

import pytest
from google.adk.tools.agent_tool import AgentTool
from google.genai import types as genai_types

from kagent.adk._sub_agent_session_plugin import SubAgentSessionPlugin
from kagent.adk.converters.part_converter import convert_genai_part_to_a2a_part
from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)


class TestSubAgentSessionPluginCallbacks:
    def _create_mock_event(self, custom_metadata=None):
        event = Mock()
        event.custom_metadata = custom_metadata
        return event

    def _create_mock_invocation_context(self):
        ctx = Mock()
        ctx.invocation_id = "inv-123"
        ctx.session = Mock()
        ctx.session.id = "session-123"
        return ctx

    def _create_mock_tool_context(self):
        context = Mock()
        context.function_call_id = "call-123"
        return context

    def _create_agent_tool(self, agent_name="test_agent"):
        agent = Mock()
        agent.name = agent_name
        agent.sub_agents = []
        return AgentTool(agent=agent)

    @pytest.mark.asyncio
    async def test_on_event_captures_context_id(self):
        plugin = SubAgentSessionPlugin()
        event = self._create_mock_event(
            custom_metadata={
                "a2a:context_id": "ctx-123",
                "a2a:task_id": "task-456",
            }
        )
        result = await plugin.on_event_callback(
            invocation_context=self._create_mock_invocation_context(),
            event=event,
        )
        assert result is None

    @pytest.mark.asyncio
    async def test_on_event_ignores_events_without_metadata(self):
        plugin = SubAgentSessionPlugin()
        result = await plugin.on_event_callback(
            invocation_context=self._create_mock_invocation_context(),
            event=self._create_mock_event(custom_metadata=None),
        )
        assert result is None

    @pytest.mark.asyncio
    async def test_on_event_ignores_events_without_a2a_keys(self):
        plugin = SubAgentSessionPlugin()
        result = await plugin.on_event_callback(
            invocation_context=self._create_mock_invocation_context(),
            event=self._create_mock_event(custom_metadata={"other_key": "value"}),
        )
        assert result is None

    @pytest.mark.asyncio
    async def test_before_tool_resets_state_for_agent_tool(self):
        plugin = SubAgentSessionPlugin()
        result = await plugin.before_tool_callback(
            tool=self._create_agent_tool(),
            tool_args={},
            tool_context=self._create_mock_tool_context(),
        )
        assert result is None

    @pytest.mark.asyncio
    async def test_before_tool_ignores_non_agent_tools(self):
        plugin = SubAgentSessionPlugin()
        result = await plugin.before_tool_callback(
            tool=Mock(spec=[]),
            tool_args={},
            tool_context=self._create_mock_tool_context(),
        )
        assert result is None

    @pytest.mark.asyncio
    async def test_after_tool_embeds_metadata_in_string_result(self):
        plugin = SubAgentSessionPlugin()
        tool = self._create_agent_tool()
        tool_context = self._create_mock_tool_context()

        await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tool_context)
        await plugin.on_event_callback(
            invocation_context=self._create_mock_invocation_context(),
            event=self._create_mock_event(
                custom_metadata={
                    "a2a:context_id": "ctx-123",
                    "a2a:task_id": "task-456",
                }
            ),
        )
        result = await plugin.after_tool_callback(
            tool=tool,
            tool_args={},
            tool_context=tool_context,
            result="agent response",
        )

        assert isinstance(result, dict)
        assert result["result"] == "agent response"
        assert result["a2a:context_id"] == "ctx-123"
        assert result["a2a:task_id"] == "task-456"

    @pytest.mark.asyncio
    async def test_after_tool_embeds_metadata_in_dict_result(self):
        plugin = SubAgentSessionPlugin()
        tool = self._create_agent_tool()
        tool_context = self._create_mock_tool_context()

        await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tool_context)
        await plugin.on_event_callback(
            invocation_context=self._create_mock_invocation_context(),
            event=self._create_mock_event(custom_metadata={"a2a:context_id": "ctx-789"}),
        )
        result = await plugin.after_tool_callback(
            tool=tool,
            tool_args={},
            tool_context=tool_context,
            result={"key": "value", "other": 42},
        )

        assert isinstance(result, dict)
        assert result["key"] == "value"
        assert result["other"] == 42
        assert result["a2a:context_id"] == "ctx-789"

    @pytest.mark.asyncio
    async def test_after_tool_returns_none_when_no_metadata(self):
        plugin = SubAgentSessionPlugin()
        tool = self._create_agent_tool()
        tool_context = self._create_mock_tool_context()

        await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tool_context)
        result = await plugin.after_tool_callback(
            tool=tool,
            tool_args={},
            tool_context=tool_context,
            result="response",
        )
        assert result is None

    @pytest.mark.asyncio
    async def test_after_tool_returns_none_for_non_agent_tool(self):
        plugin = SubAgentSessionPlugin()
        result = await plugin.after_tool_callback(
            tool=Mock(spec=[]),
            tool_args={},
            tool_context=self._create_mock_tool_context(),
            result="response",
        )
        assert result is None

    @pytest.mark.asyncio
    async def test_captures_from_first_event_only(self):
        plugin = SubAgentSessionPlugin()
        tool = self._create_agent_tool()
        tool_context = self._create_mock_tool_context()
        ctx = self._create_mock_invocation_context()

        await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tool_context)
        await plugin.on_event_callback(
            invocation_context=ctx,
            event=self._create_mock_event(
                custom_metadata={
                    "a2a:context_id": "ctx-first",
                    "a2a:task_id": "task-first",
                }
            ),
        )
        await plugin.on_event_callback(
            invocation_context=ctx,
            event=self._create_mock_event(
                custom_metadata={
                    "a2a:context_id": "ctx-second",
                    "a2a:task_id": "task-second",
                }
            ),
        )
        result = await plugin.after_tool_callback(
            tool=tool,
            tool_args={},
            tool_context=tool_context,
            result="response",
        )

        assert result["a2a:context_id"] == "ctx-first"
        assert result["a2a:task_id"] == "task-first"

    @pytest.mark.asyncio
    async def test_before_tool_resets_between_calls(self):
        plugin = SubAgentSessionPlugin()
        tool = self._create_agent_tool()
        tool_context = self._create_mock_tool_context()
        ctx = self._create_mock_invocation_context()

        await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tool_context)
        await plugin.on_event_callback(
            invocation_context=ctx,
            event=self._create_mock_event(custom_metadata={"a2a:context_id": "ctx-first"}),
        )
        result1 = await plugin.after_tool_callback(
            tool=tool,
            tool_args={},
            tool_context=tool_context,
            result="r1",
        )
        assert result1["a2a:context_id"] == "ctx-first"

        await plugin.before_tool_callback(tool=tool, tool_args={}, tool_context=tool_context)
        result2 = await plugin.after_tool_callback(
            tool=tool,
            tool_args={},
            tool_context=tool_context,
            result="r2",
        )
        assert result2 is None


class TestPartConverterMetadataExtraction:
    """Test cases for convert_genai_part_to_a2a_part() metadata extraction."""

    def test_metadata_extracted_from_function_response(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:context_id": "ctx-123",
                "a2a:task_id": "task-456",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata is not None
        assert a2a_part.root.metadata["a2a:context_id"] == "ctx-123"
        assert a2a_part.root.metadata["a2a:task_id"] == "task-456"

        assert "a2a:context_id" not in a2a_part.root.data["response"]
        assert "a2a:task_id" not in a2a_part.root.data["response"]
        assert a2a_part.root.data["response"]["result"] == "ok"

    def test_metadata_only_context_id_extracted(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:context_id": "ctx-only",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata["a2a:context_id"] == "ctx-only"
        assert "a2a:task_id" not in a2a_part.root.metadata
        assert "a2a:context_id" not in a2a_part.root.data["response"]

    def test_metadata_only_task_id_extracted(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:task_id": "task-only",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata["a2a:task_id"] == "task-only"
        assert "a2a:context_id" not in a2a_part.root.metadata
        assert "a2a:task_id" not in a2a_part.root.data["response"]

    def test_no_metadata_when_not_present(self):
        """Test that DataPart metadata only has type key when no embedded metadata."""
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={"result": "ok"},
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata is not None
        # Should only have the type key
        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY) in a2a_part.root.metadata
        assert (
            a2a_part.root.metadata[get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY)]
            == A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE
        )
        assert "a2a:context_id" not in a2a_part.root.metadata
        assert "a2a:task_id" not in a2a_part.root.metadata

    def test_metadata_keys_cleaned_from_response_dict(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "other_key": "other_value",
                "a2a:context_id": "ctx-123",
                "a2a:task_id": "task-456",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        response_data = a2a_part.root.data["response"]

        assert "a2a:context_id" not in response_data
        assert "a2a:task_id" not in response_data
        assert response_data["result"] == "ok"
        assert response_data["other_key"] == "other_value"

    def test_metadata_extraction_with_nested_response_dict(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "nested": {
                    "result": "ok",
                },
                "a2a:context_id": "ctx-nested",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.data["response"]["nested"]["result"] == "ok"
        assert "a2a:context_id" not in a2a_part.root.data["response"]
        assert a2a_part.root.metadata["a2a:context_id"] == "ctx-nested"

    def test_metadata_extraction_with_empty_response_dict(self):
        """Test that extraction handles empty response dict."""
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={},
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.data["response"] == {}
        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY) in a2a_part.root.metadata
        assert "a2a:context_id" not in a2a_part.root.metadata
        assert "a2a:task_id" not in a2a_part.root.metadata

    def test_metadata_extraction_preserves_other_metadata(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:context_id": "ctx-123",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        metadata = a2a_part.root.metadata

        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY) in metadata
        assert (
            metadata[get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY)]
            == A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE
        )
        assert metadata["a2a:context_id"] == "ctx-123"
