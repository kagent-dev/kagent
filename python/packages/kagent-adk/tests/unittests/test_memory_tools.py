import json
from types import SimpleNamespace
from typing import cast
from unittest.mock import AsyncMock

import httpx
import pytest
from google.adk.memory.memory_entry import MemoryEntry
from google.adk.tools import ToolContext
from google.genai import types

from kagent.adk._memory_service import KagentMemoryService
from kagent.adk.tools.memory_tools import SaveMemoryTool


@pytest.mark.asyncio
async def test_add_memory_stores_adk_memory_entries_with_metadata():
    requests: list[dict] = []

    def controller(request: httpx.Request) -> httpx.Response:
        requests.append(json.loads(request.content))
        return httpx.Response(201, json={"id": f"memory-{len(requests)}"})

    embedding_client = SimpleNamespace(
        generate=AsyncMock(return_value=[[0.1, 0.2], [0.3, 0.4]]),
    )
    memories = [
        MemoryEntry(
            content=types.Content(role="user", parts=[types.Part(text="first memory")]),
            custom_metadata={"scope": "entry", "shared": "entry wins"},
        ),
        MemoryEntry(
            content=types.Content(role="user", parts=[types.Part(text="second"), types.Part(text="memory")]),
        ),
    ]

    async with httpx.AsyncClient(transport=httpx.MockTransport(controller), base_url="http://controller") as client:
        service = KagentMemoryService(agent_name="agent", http_client=client, ttl_days=7)
        service._embedding_client = embedding_client
        await service.add_memory(
            app_name="app",
            user_id="user",
            memories=memories,
            custom_metadata={"shared": "default"},
        )

    embedding_client.generate.assert_awaited_once_with(["first memory", "second\nmemory"])
    assert requests == [
        {
            "agent_name": "agent",
            "user_id": "user",
            "content": "first memory",
            "vector": [0.1, 0.2],
            "metadata": {"shared": "entry wins", "scope": "entry"},
            "ttl_days": 7,
        },
        {
            "agent_name": "agent",
            "user_id": "user",
            "content": "second\nmemory",
            "vector": [0.3, 0.4],
            "metadata": {"shared": "default"},
            "ttl_days": 7,
        },
    ]


@pytest.mark.asyncio
async def test_add_memory_propagates_storage_errors():
    def controller(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, text="storage unavailable")

    async with httpx.AsyncClient(transport=httpx.MockTransport(controller), base_url="http://controller") as client:
        service = KagentMemoryService(agent_name="agent", http_client=client)
        service._embedding_client = SimpleNamespace(generate=AsyncMock(return_value=[[0.1, 0.2]]))

        with pytest.raises(httpx.HTTPStatusError):
            await service.add_memory(
                app_name="app",
                user_id="user",
                memories=[MemoryEntry(content=types.Content(role="user", parts=[types.Part(text="remember")]))],
            )


@pytest.mark.asyncio
async def test_save_memory_tool_uses_adk_memory_service_interface():
    memory_service = SimpleNamespace(add_memory=AsyncMock())
    context = cast(
        ToolContext,
        SimpleNamespace(
            _invocation_context=SimpleNamespace(memory_service=memory_service),
            session=SimpleNamespace(id="session", app_name="app", user_id="user"),
        ),
    )

    result = await SaveMemoryTool().run_async(args={"content": "remember this"}, tool_context=context)

    assert result == "Successfully saved information to long-term memory."
    memory_service.add_memory.assert_awaited_once()
    call = memory_service.add_memory.await_args
    assert call.kwargs["app_name"] == "app"
    assert call.kwargs["user_id"] == "user"
    assert "custom_metadata" not in call.kwargs
    memories = call.kwargs["memories"]
    assert len(memories) == 1
    assert memories[0].content.parts[0].text == "remember this"
    assert memories[0].custom_metadata == {"session_id": "session", "source": "explicit_save"}
