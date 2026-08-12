from unittest.mock import AsyncMock, MagicMock

import pytest
from a2a.types import ListTasksRequest, Task, TaskState, TaskStatus
from a2a.types import a2a_pb2
from google.protobuf.timestamp_pb2 import Timestamp
from kagent.api.v1alpha1 import sessions_pb2

from kagent.core.a2a import KAgentTaskStore


def _client(*tasks: Task) -> MagicMock:
    client = MagicMock()
    client.max_message_bytes = 16 << 20
    client.call_options = AsyncMock(return_value={})
    client.task_service.ListTasks = AsyncMock(
        return_value=sessions_pb2.ListTasksResponse(
            tasks=[a2a_pb2.Task.FromString(task.SerializeToString()) for task in tasks]
        )
    )
    return client


@pytest.mark.asyncio
async def test_list_requires_context_id():
    client = _client()
    store = KAgentTaskStore(client)

    response = await store.list(ListTasksRequest())

    assert len(response.tasks) == 0
    assert response.total_size == 0
    client.task_service.ListTasks.assert_not_awaited()


@pytest.mark.asyncio
async def test_list_filters_status_and_supports_paging():
    timestamp = Timestamp()
    timestamp.GetCurrentTime()
    task_working = Task(
        id="t-working",
        context_id="ctx-1",
        status=TaskStatus(state=TaskState.TASK_STATE_WORKING, timestamp=timestamp),
    )
    task_done = Task(
        id="t-done",
        context_id="ctx-1",
        status=TaskStatus(state=TaskState.TASK_STATE_COMPLETED, timestamp=timestamp),
    )
    client = _client(task_working, task_done)
    store = KAgentTaskStore(client)

    response = await store.list(
        ListTasksRequest(
            context_id="ctx-1",
            status=TaskState.TASK_STATE_WORKING,
            page_size=1,
        )
    )

    assert response.total_size == 1
    assert response.page_size == 1
    assert len(response.tasks) == 1
    assert response.tasks[0].id == "t-working"
    client.task_service.ListTasks.assert_awaited_once()
