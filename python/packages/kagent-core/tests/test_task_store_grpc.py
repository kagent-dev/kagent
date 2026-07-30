from __future__ import annotations

import grpc
import pytest
from a2a.types import (
    Artifact,
    DataPart,
    FilePart,
    FileWithBytes,
    FileWithUri,
    Message,
    Part,
    Role,
    Task,
    TaskState,
    TaskStatus,
    TextPart,
)
from kagent.api.v1alpha1 import sessions_pb2, sessions_pb2_grpc

from kagent.core import AsyncControllerClient
from kagent.core._structured_object import decode_structured_object, encode_structured_object
from kagent.core.a2a import KAgentTaskStore, set_request_user_id


class _TokenProvider:
    async def get_token(self) -> str:
        return "test-token"


class _TaskService(sessions_pb2_grpc.TaskServiceServicer):
    def __init__(self) -> None:
        self.task = None
        self.metadata = None
        self.had_deadline = False
        self.deleted_task_id = None

    async def CreateTask(self, request, context):
        self.task = request.task
        self.metadata = dict(context.invocation_metadata())
        self.had_deadline = context.time_remaining() is not None
        return sessions_pb2.CreateTaskResponse(task=request.task)

    async def GetTask(self, request, context):
        if request.task_id == "missing":
            await context.abort(grpc.StatusCode.NOT_FOUND, "task not found")
        return sessions_pb2.GetTaskResponse(task=self.task)

    async def DeleteTask(self, request, context):
        self.deleted_task_id = request.task_id
        return sessions_pb2.DeleteTaskResponse()


@pytest.fixture
async def task_service():
    service = _TaskService()
    server = grpc.aio.server()
    sessions_pb2_grpc.add_TaskServiceServicer_to_server(service, server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    client = AsyncControllerClient(
        f"127.0.0.1:{port}",
        agent_name="test-agent",
        token_provider=_TokenProvider(),
    )
    try:
        yield service, client
    finally:
        await client.close()
        await server.stop(None)


def _message(message_id: str, text: str, *, partial: bool = False) -> Message:
    metadata = {"adk_partial": True} if partial else None
    return Message(
        role=Role.user,
        message_id=message_id,
        parts=[Part(TextPart(text=text))],
        metadata=metadata,
    )


@pytest.mark.asyncio
async def test_task_store_uses_generated_rpc_and_canonical_task(task_service):
    service, client = task_service
    store = KAgentTaskStore(client)
    task = Task(
        id="task-1",
        context_id="context-1",
        status=TaskStatus(state=TaskState.completed),
        history=[_message("partial", "skip", partial=True), _message("kept", "keep")],
        artifacts=[
            Artifact(
                artifact_id="partial-artifact",
                parts=[Part(TextPart(text="skip"))],
                metadata={"adk_partial": True},
            ),
            Artifact(artifact_id="kept-artifact", parts=[Part(TextPart(text="keep"))]),
        ],
    )
    set_request_user_id("user-1")

    await store.save(task)

    assert service.metadata == {
        "authorization": "Bearer test-token",
        "user-agent": service.metadata["user-agent"],
        "x-agent-name": "test-agent",
        "x-user-id": "user-1",
    }
    assert service.had_deadline is True
    assert service.task.api_version == "lf.a2a.v1"
    assert service.task.kind == "Task"
    payload = decode_structured_object(service.task, expected_kind="Task", max_bytes=16 << 20)
    assert payload["contextId"] == "context-1"
    assert [message["messageId"] for message in payload["history"]] == ["kept"]
    assert [artifact["artifactId"] for artifact in payload["artifacts"]] == ["kept-artifact"]
    assert [message.message_id for message in task.history or []] == ["partial", "kept"]
    assert [artifact.artifact_id for artifact in task.artifacts or []] == ["partial-artifact", "kept-artifact"]

    restored = await store.get("task-1")
    assert restored is not None
    assert restored.id == "task-1"
    assert [message.message_id for message in restored.history or []] == ["kept"]

    await store.delete("task-1")
    assert service.deleted_task_id == "task-1"


@pytest.mark.asyncio
async def test_task_store_writes_canonical_go_a2a_task(task_service):
    service, client = task_service
    store = KAgentTaskStore(client)
    task = Task(
        id="task-python",
        context_id="context-python",
        status=TaskStatus(
            state=TaskState.completed,
            message=Message(
                message_id="status-message",
                role=Role.agent,
                parts=[Part(TextPart(text="done"))],
            ),
        ),
        history=[
            Message(
                message_id="user-message",
                role=Role.user,
                parts=[
                    Part(TextPart(text="hello")),
                    Part(DataPart(data={"answer": 42})),
                    Part(
                        FilePart(
                            file=FileWithUri(
                                uri="https://example.com/result.txt",
                                name="result.txt",
                                mime_type="text/plain",
                            )
                        )
                    ),
                ],
            )
        ],
        artifacts=[
            Artifact(
                artifact_id="artifact-1",
                parts=[
                    Part(
                        FilePart(
                            file=FileWithBytes(
                                bytes="AQI=",
                                mime_type="application/octet-stream",
                            )
                        )
                    )
                ],
            )
        ],
    )

    await store.save(task)

    payload = decode_structured_object(service.task, expected_kind="Task", max_bytes=16 << 20)
    assert payload["status"]["state"] == "TASK_STATE_COMPLETED"
    assert payload["status"]["message"]["role"] == "ROLE_AGENT"
    assert payload["status"]["message"]["parts"] == [{"text": "done"}]
    assert payload["history"][0]["role"] == "ROLE_USER"
    assert payload["history"][0]["parts"] == [
        {"text": "hello"},
        {"data": {"answer": 42}},
        {
            "url": "https://example.com/result.txt",
            "filename": "result.txt",
            "mediaType": "text/plain",
        },
    ]
    assert payload["artifacts"][0]["parts"] == [{"raw": "AQI=", "mediaType": "application/octet-stream"}]


@pytest.mark.asyncio
async def test_task_store_reads_canonical_go_a2a_task(task_service):
    service, client = task_service
    service.task = encode_structured_object(
        {
            "id": "task-go",
            "contextId": "context-go",
            "status": {
                "state": "TASK_STATE_COMPLETED",
                "message": {
                    "messageId": "status-message",
                    "role": "ROLE_AGENT",
                    "parts": [{"text": "done"}],
                },
            },
            "history": [
                {
                    "messageId": "user-message",
                    "role": "ROLE_USER",
                    "parts": [
                        {"text": "hello"},
                        {"data": {"answer": 42}},
                        {
                            "url": "https://example.com/result.txt",
                            "filename": "result.txt",
                            "mediaType": "text/plain",
                        },
                    ],
                }
            ],
            "artifacts": [
                {
                    "artifactId": "artifact-1",
                    "parts": [{"raw": "AQI=", "mediaType": "application/octet-stream"}],
                }
            ],
        },
        api_version="lf.a2a.v1",
        kind="Task",
        max_bytes=16 << 20,
    )
    store = KAgentTaskStore(client)

    restored = await store.get("task-go")

    assert restored is not None
    assert restored.status.state == TaskState.completed
    assert restored.status.message is not None
    assert restored.status.message.role == Role.agent
    assert restored.history is not None
    assert restored.history[0].role == Role.user
    assert [part.root.kind for part in restored.history[0].parts] == ["text", "data", "file"]
    assert restored.artifacts is not None
    assert restored.artifacts[0].parts[0].root.file.bytes == "AQI="


@pytest.mark.asyncio
async def test_task_store_maps_not_found_to_none(task_service):
    _, client = task_service
    store = KAgentTaskStore(client)

    assert await store.get("missing") is None


@pytest.mark.asyncio
async def test_controller_client_does_not_close_injected_channel():
    channel = grpc.aio.insecure_channel("127.0.0.1:1")
    client = AsyncControllerClient(channel=channel)

    await client.close()
    await client.close()

    assert channel.get_state(try_to_connect=False) is not grpc.ChannelConnectivity.SHUTDOWN
    await channel.close()
