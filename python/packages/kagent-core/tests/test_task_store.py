import httpx
import pytest
from a2a.types import ListTasksRequest, Task, TaskState, TaskStatus
from google.protobuf.json_format import MessageToDict
from google.protobuf.timestamp_pb2 import Timestamp

from kagent.core.a2a import KAgentTaskStore


@pytest.mark.asyncio
async def test_list_requires_context_id():
    client = httpx.AsyncClient(base_url="http://kagent.local")
    store = KAgentTaskStore(client)

    resp = await store.list(ListTasksRequest())
    assert len(resp.tasks) == 0
    assert resp.total_size == 0

    await client.aclose()


@pytest.mark.asyncio
async def test_list_filters_status_and_supports_paging():
    ts = Timestamp()
    ts.GetCurrentTime()
    task_working = Task(
        id="t-working",
        context_id="ctx-1",
        status=TaskStatus(state=TaskState.TASK_STATE_WORKING, timestamp=ts),
    )
    task_done = Task(
        id="t-done",
        context_id="ctx-1",
        status=TaskStatus(state=TaskState.TASK_STATE_COMPLETED, timestamp=ts),
    )
    payload = {
        "data": [MessageToDict(task_working), MessageToDict(task_done)],
    }

    async def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/api/sessions/ctx-1/tasks"
        return httpx.Response(200, json=payload)

    transport = httpx.MockTransport(handler)
    client = httpx.AsyncClient(base_url="http://kagent.local", transport=transport)
    store = KAgentTaskStore(client)

    resp = await store.list(
        ListTasksRequest(
            context_id="ctx-1",
            status=TaskState.TASK_STATE_WORKING,
            page_size=1,
        )
    )

    assert resp.total_size == 1
    assert resp.page_size == 1
    assert len(resp.tasks) == 1
    assert resp.tasks[0].id == "t-working"

    await client.aclose()
