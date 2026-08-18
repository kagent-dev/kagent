from unittest.mock import AsyncMock, MagicMock

import pytest
from a2a.server.context import ServerCallContext

from kagent.core.a2a import KAgentTaskStore
from kagent.core.a2a._context import get_request_user_id, set_request_user_id


@pytest.mark.asyncio
async def test_get_scopes_user_from_server_call_context():
    observed_users: list[str | None] = []
    response = MagicMock(status_code=404)

    async def get_task(_: str):
        observed_users.append(get_request_user_id())
        return response

    client = MagicMock()
    client.get = AsyncMock(side_effect=get_task)
    store = KAgentTaskStore(client)
    context = ServerCallContext(state={"headers": {"x-user-id": "user-1"}})
    set_request_user_id("ambient-user")
    try:
        result = await store.get("task-1", context=context)

        assert result is None
        assert observed_users == ["user-1"]
        assert get_request_user_id() == "ambient-user"
    finally:
        set_request_user_id(None)


@pytest.mark.asyncio
async def test_get_restores_user_when_request_fails():
    client = MagicMock()
    client.get = AsyncMock(side_effect=RuntimeError("controller unavailable"))
    store = KAgentTaskStore(client)
    context = ServerCallContext(state={"headers": {"x-user-id": "user-1"}})
    set_request_user_id("ambient-user")
    try:
        with pytest.raises(RuntimeError, match="controller unavailable"):
            await store.get("task-1", context=context)

        assert get_request_user_id() == "ambient-user"
    finally:
        set_request_user_id(None)


@pytest.mark.asyncio
async def test_get_without_scoped_user_preserves_ambient_user():
    observed_users: list[str | None] = []

    async def get_task(_: str):
        observed_users.append(get_request_user_id())
        return MagicMock(status_code=404)

    client = MagicMock()
    client.get = AsyncMock(side_effect=get_task)
    store = KAgentTaskStore(client)
    context = ServerCallContext(state={"headers": {}})
    set_request_user_id("ambient-user")
    try:
        result = await store.get("task-1", context=context)

        assert result is None
        assert observed_users == ["ambient-user"]
        assert get_request_user_id() == "ambient-user"
    finally:
        set_request_user_id(None)
