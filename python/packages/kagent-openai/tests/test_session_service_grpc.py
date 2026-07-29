import json
from unittest.mock import AsyncMock, MagicMock

import grpc
import pytest
from kagent.api.v1alpha1 import sessions_pb2

from kagent.openai._session_service import KAgentSession


@pytest.fixture
def client():
    value = MagicMock()
    value.call_options = AsyncMock(return_value={"metadata": (), "timeout": 30.0})
    value.session_service = MagicMock()
    value.session_service.GetSession = AsyncMock()
    value.session_service.CreateSession = AsyncMock(
        return_value=sessions_pb2.CreateSessionResponse(
            session=sessions_pb2.Session(id="session-1", user_id="user-1")
        )
    )
    value.session_service.AddSessionEvent = AsyncMock(
        return_value=sessions_pb2.AddSessionEventResponse()
    )
    value.session_service.DeleteSession = AsyncMock(
        return_value=sessions_pb2.DeleteSessionResponse()
    )
    return value


@pytest.fixture
def session(client):
    return KAgentSession(
        session_id="session-1",
        client=client,
        app_name="default__NS__openai-agent",
        user_id="user-1",
    )


def _rpc_error(code: grpc.StatusCode, details: str) -> grpc.aio.AioRpcError:
    return grpc.aio.AioRpcError(code, (), (), details, "")


@pytest.mark.asyncio
async def test_get_items_replays_generated_events_chronologically_and_limits_items(session, client):
    client.session_service.GetSession.return_value = sessions_pb2.GetSessionResponse(
        session=sessions_pb2.Session(id="session-1", user_id="user-1"),
        events=[
            sessions_pb2.SessionEvent(data=json.dumps({"items": [{"role": "user", "content": "one"}]})),
            sessions_pb2.SessionEvent(
                data=json.dumps(
                    {
                        "items": [
                            {"role": "assistant", "content": "two"},
                            {"role": "user", "content": "three"},
                        ]
                    }
                )
            ),
        ],
    )

    items = await session.get_items(limit=2)

    assert [item["content"] for item in items] == ["two", "three"]
    request = client.session_service.GetSession.await_args.args[0]
    assert request.session_id == "session-1"
    assert request.order == sessions_pb2.EVENT_ORDER_ASCENDING
    assert not request.HasField("limit")
    client.call_options.assert_awaited_once_with("user-1")


@pytest.mark.asyncio
async def test_add_items_creates_missing_session_then_adds_generated_event(session, client):
    client.session_service.GetSession.side_effect = _rpc_error(grpc.StatusCode.NOT_FOUND, "missing")
    items = [{"role": "user", "content": "hello"}]

    await session.add_items(items)

    create_request = client.session_service.CreateSession.await_args.args[0]
    assert create_request.id == "session-1"
    assert create_request.agent_ref == "default__NS__openai-agent"
    event_request = client.session_service.AddSessionEvent.await_args.args[0]
    assert event_request.session_id == "session-1"
    payload = json.loads(event_request.data)
    assert payload["type"] == "conversation_items"
    assert payload["items"] == items
    assert client.call_options.await_args_list == [
        (("user-1",), {}),
        (("user-1",), {}),
        (("user-1",), {}),
    ]


@pytest.mark.asyncio
async def test_get_and_clear_map_not_found_to_empty_success(session, client):
    client.session_service.GetSession.side_effect = _rpc_error(grpc.StatusCode.NOT_FOUND, "missing")
    client.session_service.DeleteSession.side_effect = _rpc_error(grpc.StatusCode.NOT_FOUND, "missing")

    assert await session.get_items() == []
    await session.clear_session()

    assert session._items_cache is None
