import json
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from kagent.adk.memory import McpMemoryService


def _make_tool(name: str, run_return=None):
    tool = MagicMock()
    tool.name = name
    tool.run = AsyncMock(return_value=run_return)
    return tool


def _make_service_with_tools(*tools):
    with patch("kagent.adk.memory.KAgentMcpToolset") as mock_cls:
        instance = mock_cls.return_value
        instance.get_tools = AsyncMock(return_value=list(tools))
        service = McpMemoryService(connection_params=MagicMock())
    return service


def _make_session_mock(dump_data=None):
    session = MagicMock()
    session.model_dump = MagicMock(return_value=dump_data or {"id": "s1", "events": []})
    return session


class TestSearchMemoryDictResult:
    @pytest.mark.asyncio
    async def test_properly_structured_memories(self):
        result = {
            "memories": [
                {
                    "content": {"parts": [{"text": "hello"}], "role": "user"},
                }
            ]
        }
        search_tool = _make_tool("search_memory", run_return=result)
        service = _make_service_with_tools(search_tool)

        response = await service.search_memory(app_name="app", user_id="u1", query="q")

        assert len(response.memories) == 1
        assert response.memories[0].content.parts[0].text == "hello"
        assert response.memories[0].content.role == "user"

    @pytest.mark.asyncio
    async def test_text_without_content_auto_wraps(self):
        result = {"memories": [{"text": "some memory text"}]}
        search_tool = _make_tool("search_memory", run_return=result)
        service = _make_service_with_tools(search_tool)

        response = await service.search_memory(app_name="app", user_id="u1", query="q")

        assert len(response.memories) == 1
        assert response.memories[0].content.parts[0].text == "some memory text"
        assert response.memories[0].content.role == "user"

    @pytest.mark.asyncio
    async def test_raw_string_content_auto_wraps(self):
        result = {"memories": [{"content": "raw string content"}]}
        search_tool = _make_tool("search_memory", run_return=result)
        service = _make_service_with_tools(search_tool)

        response = await service.search_memory(app_name="app", user_id="u1", query="q")

        assert len(response.memories) == 1
        assert response.memories[0].content.parts[0].text == "raw string content"
        assert response.memories[0].content.role == "user"


class TestSearchMemoryStringResult:
    @pytest.mark.asyncio
    async def test_valid_json_string_parsed(self):
        data = {"memories": [{"content": {"parts": [{"text": "from json"}], "role": "model"}}]}
        search_tool = _make_tool("search_memory", run_return=json.dumps(data))
        service = _make_service_with_tools(search_tool)

        response = await service.search_memory(app_name="app", user_id="u1", query="q")

        assert len(response.memories) == 1
        assert response.memories[0].content.parts[0].text == "from json"

    @pytest.mark.asyncio
    async def test_invalid_json_string_raises(self):
        search_tool = _make_tool("search_memory", run_return="not valid json {{{")
        service = _make_service_with_tools(search_tool)

        with pytest.raises(ValueError, match="Unexpected result type"):
            await service.search_memory(app_name="app", user_id="u1", query="q")


class TestSearchMemoryModelDumpResult:
    @pytest.mark.asyncio
    async def test_result_with_model_dump(self):
        inner = {"memories": [{"content": {"parts": [{"text": "dumped"}], "role": "user"}}]}
        result_obj = MagicMock()
        result_obj.model_dump = MagicMock(return_value=inner)
        result_obj.__class__ = type("CustomModel", (), {})

        search_tool = _make_tool("search_memory", run_return=result_obj)
        service = _make_service_with_tools(search_tool)

        response = await service.search_memory(app_name="app", user_id="u1", query="q")

        result_obj.model_dump.assert_called_once()
        assert len(response.memories) == 1
        assert response.memories[0].content.parts[0].text == "dumped"


class TestSearchMemoryUnexpectedResult:
    @pytest.mark.asyncio
    async def test_unexpected_type_raises(self):
        search_tool = _make_tool("search_memory", run_return=42)
        service = _make_service_with_tools(search_tool)

        with pytest.raises(ValueError, match="Unexpected result type"):
            await service.search_memory(app_name="app", user_id="u1", query="q")


class TestAddSessionToMemory:
    @pytest.mark.asyncio
    async def test_successful_call(self):
        add_tool = _make_tool("add_session_to_memory", run_return=None)
        service = _make_service_with_tools(add_tool)
        session = _make_session_mock({"id": "s1", "events": [{"type": "msg"}]})

        await service.add_session_to_memory(session)

        session.model_dump.assert_called_once_with(mode="json")
        add_tool.run.assert_awaited_once_with(session={"id": "s1", "events": [{"type": "msg"}]})

    @pytest.mark.asyncio
    async def test_tool_exception_propagates(self):
        add_tool = _make_tool("add_session_to_memory")
        add_tool.run = AsyncMock(side_effect=RuntimeError("server down"))
        service = _make_service_with_tools(add_tool)
        session = _make_session_mock()

        with pytest.raises(RuntimeError, match="server down"):
            await service.add_session_to_memory(session)


class TestCallToolNotFound:
    @pytest.mark.asyncio
    async def test_missing_tool_raises(self):
        some_tool = _make_tool("other_tool")
        service = _make_service_with_tools(some_tool)

        with pytest.raises(ValueError, match="not found in MCP server"):
            await service.search_memory(app_name="app", user_id="u1", query="q")


class TestEnsureToolsCaching:
    @pytest.mark.asyncio
    async def test_tools_fetched_only_once(self):
        search_tool = _make_tool("search_memory", run_return={"memories": []})
        add_tool = _make_tool("add_session_to_memory", run_return=None)

        with patch("kagent.adk.memory.KAgentMcpToolset") as mock_cls:
            instance = mock_cls.return_value
            instance.get_tools = AsyncMock(return_value=[search_tool, add_tool])
            service = McpMemoryService(connection_params=MagicMock())

            await service.search_memory(app_name="a", user_id="u", query="q")
            await service.search_memory(app_name="a", user_id="u", query="q2")
            session = _make_session_mock()
            await service.add_session_to_memory(session)

            instance.get_tools.assert_awaited_once()
