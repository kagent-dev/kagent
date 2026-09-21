from unittest.mock import AsyncMock, MagicMock

import pytest
from a2a.server.agent_execution import SimpleRequestContextBuilder
from a2a.server.context import ServerCallContext

from kagent.core.a2a import KAgentRequestContextBuilder
from kagent.core.a2a._context import get_request_user_id, set_request_user_id


@pytest.mark.asyncio
async def test_headerless_request_clears_previous_user(monkeypatch):
    monkeypatch.setattr(SimpleRequestContextBuilder, "build", AsyncMock(return_value=MagicMock()))
    builder = KAgentRequestContextBuilder(task_store=MagicMock())

    set_request_user_id(None)
    try:
        await builder.build(context=ServerCallContext(state={"headers": {"x-user-id": "user-1"}}))
        assert get_request_user_id() == "user-1"

        await builder.build(context=ServerCallContext(state={"headers": {}}))
        assert get_request_user_id() is None
    finally:
        set_request_user_id(None)
