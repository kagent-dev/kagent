"""Pinned SDK tests over gRPC; PostgreSQL contract coverage lives in Go."""

import asyncio
from pathlib import Path
from uuid import uuid4

import grpc
import pytest
from a2a.server.agent_execution import AgentExecutor
from a2a.server.context import ServerCallContext
from a2a.server.events.event_queue import DEFAULT_MAX_QUEUE_SIZE
from a2a.types import a2a_pb2 as a2a
from a2a.utils.errors import InternalError, InvalidParamsError
from kagent.api.v1alpha1 import task_store_pb2 as storepb
from kagent.api.v1alpha1 import task_store_pb2_grpc as storerpc

from kagent.core._grpc import AsyncControllerClient
from kagent.core.a2a._task_store import KAgentRequestHandler, KAgentTaskStore


class Storage(storerpc.TaskStoreServiceServicer):
    def __init__(self, instance_id):
        self.instance_id = instance_id
        self.task = None
        self.version = 0
        self.inputs = set()
        self.receipts = {}
        self.lose_save = True
        self.reject_saves = False
        self.settlements = []
        self.admission_committed = asyncio.Event()
        self.admission_release = asyncio.Event()
        self.admission_release.set()

    async def AdmitMessage(self, request, context):
        assert request.agent_instance_id == self.instance_id
        message = request.request.message
        previous = None
        admitted = message.message_id not in self.inputs
        if admitted:
            self.inputs.add(message.message_id)
            if self.task is None:
                self.task = a2a.Task(id="task", context_id=self.instance_id)
            else:
                previous = a2a.Task()
                previous.CopyFrom(self.task)
                if self.task.status.HasField("message"):
                    self.task.history.append(self.task.status.message)
            message.task_id, message.context_id = self.task.id, self.task.context_id
            self.task.history.append(message)
            self.task.status.CopyFrom(a2a.TaskStatus(state=a2a.TASK_STATE_SUBMITTED))
            self.version += 1
        response = storepb.TaskStoreServiceAdmitMessageResponse(
            current=storepb.StoredTask(task=self.task, version=self.version), previous=previous, admitted=admitted
        )
        self.admission_committed.set()
        await self.admission_release.wait()
        return response

    async def GetTask(self, request, context):
        if self.task is None or request.task_id != self.task.id:
            await context.abort(grpc.StatusCode.NOT_FOUND, "absent")
        return storepb.TaskStoreServiceGetTaskResponse(stored=storepb.StoredTask(task=self.task, version=self.version))

    async def UpdateTask(self, request, context):
        if self.reject_saves:
            await context.abort(grpc.StatusCode.UNAVAILABLE, "storage outage")
        assert dict(context.invocation_metadata())["authorization"] == "Bearer substrate-actor"
        key = request.expected_version
        payload = request.SerializeToString(deterministic=True)
        if key in self.receipts:
            digest, version = self.receipts[key]
            if digest != payload:
                await context.abort(grpc.StatusCode.ABORTED, "conflicting retry")
            return storepb.TaskStoreServiceUpdateTaskResponse(version=version)
        if key != self.version:
            await context.abort(grpc.StatusCode.ABORTED, "stale save")
        self.task.CopyFrom(request.task)
        self.version += 1
        self.receipts[key] = payload, self.version
        if self.lose_save:
            self.lose_save = False
            await context.abort(grpc.StatusCode.UNAVAILABLE, "response lost after commit")
        return storepb.TaskStoreServiceUpdateTaskResponse(version=self.version)

    async def SettleTask(self, request, context):
        assert request.version <= self.version
        self.settlements.append(request.version)
        return storepb.TaskStoreServiceSettleTaskResponse()


class Runner(AgentExecutor):
    def __init__(self):
        self.release = asyncio.Event()
        self.calls = 0
        self.stopped = asyncio.Event()
        self.cleanup_started = asyncio.Event()
        self.cleanup_release = asyncio.Event()
        self.cleanup_release.set()
        self.rich_history = False
        self.progress_events = 0

    async def execute(self, context, events):
        self.calls += 1
        if self.rich_history and self.calls > 1:
            await events.enqueue_event(
                a2a.TaskArtifactUpdateEvent(
                    task_id=context.task_id,
                    context_id=context.context_id,
                    artifact=a2a.Artifact(artifact_id="answer", parts=[a2a.Part(text="resumed answer")]),
                )
            )
        await events.enqueue_event(
            a2a.TaskStatusUpdateEvent(
                task_id=context.task_id,
                context_id=context.context_id,
                status=a2a.TaskStatus(state=a2a.TASK_STATE_WORKING),
            )
        )
        try:
            await self.release.wait()
        except asyncio.CancelledError:
            self.cleanup_started.set()
            await self.cleanup_release.wait()
            self.stopped.set()
            raise
        state = a2a.TASK_STATE_INPUT_REQUIRED if self.calls == 1 else a2a.TASK_STATE_COMPLETED
        for _ in range(self.progress_events):
            await events.enqueue_event(
                a2a.TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    context_id=context.context_id,
                    status=a2a.TaskStatus(state=a2a.TASK_STATE_WORKING),
                )
            )
        if self.rich_history and self.calls == 1:
            # The SDK caches both copies; persisted history deduplicates IDs.
            for _ in range(2):
                await events.enqueue_event(
                    a2a.TaskStatusUpdateEvent(
                        task_id=context.task_id,
                        context_id=context.context_id,
                        status=a2a.TaskStatus(
                            state=a2a.TASK_STATE_WORKING,
                            message=a2a.Message(
                                message_id="progress",
                                role=a2a.ROLE_AGENT,
                                task_id=context.task_id,
                                context_id=context.context_id,
                                parts=[a2a.Part(text="preparing answer")],
                            ),
                        ),
                    )
                )
            await events.enqueue_event(
                a2a.TaskArtifactUpdateEvent(
                    task_id=context.task_id,
                    context_id=context.context_id,
                    artifact=a2a.Artifact(artifact_id="answer", parts=[a2a.Part(text="partial answer", metadata={})]),
                )
            )
        await events.enqueue_event(
            a2a.TaskStatusUpdateEvent(
                task_id=context.task_id,
                context_id=context.context_id,
                status=a2a.TaskStatus(
                    state=state,
                    message=a2a.Message(
                        message_id=f"output-{self.calls}",
                        task_id=context.task_id,
                        context_id=context.context_id,
                        role=a2a.ROLE_AGENT,
                        parts=[a2a.Part(text="continue?" if self.calls == 1 else "done")],
                    ),
                ),
            )
        )

    async def cancel(self, context, events):
        await events.enqueue_event(
            a2a.TaskStatusUpdateEvent(
                task_id=context.task_id,
                context_id=context.context_id,
                status=a2a.TaskStatus(state=a2a.TASK_STATE_CANCELED),
            )
        )


def send(message_id, task_id=""):
    return a2a.SendMessageRequest(
        message=a2a.Message(message_id=message_id, task_id=task_id, role=a2a.ROLE_USER, parts=[a2a.Part(text="go")])
    )


@pytest.fixture
async def runtime(tmp_path: Path):
    instance_id = str(uuid4())
    identity = tmp_path / "name"
    identity.write_text("ai-" + instance_id)
    service = Storage(instance_id)
    server = grpc.aio.server()
    storerpc.add_TaskStoreServiceServicer_to_server(service, server)
    port = server.add_insecure_port("127.0.0.1:0")
    await server.start()
    client = AsyncControllerClient(f"http://127.0.0.1:{port}")
    store = KAgentTaskStore(client, identity)
    runner = Runner()
    handler = KAgentRequestHandler(
        agent_executor=runner,
        task_store=store,
        agent_card=a2a.AgentCard(capabilities=a2a.AgentCapabilities(streaming=True)),
    )
    try:
        yield service, store, runner, handler
    finally:
        await handler.aclose()
        await client.close()
        await server.stop(None)


async def eventually(predicate, timeout=5):
    async with asyncio.timeout(timeout):
        while not predicate():  # noqa: ASYNC110 - observe an external gRPC commit
            await asyncio.sleep(0.01)


async def test_failed_persistence_stops_native_execution(runtime):
    service, _, runner, handler = runtime
    service.reject_saves = True
    stream = handler.on_message_send_stream(send("storage-failure"), ServerCallContext())
    async with asyncio.timeout(5):
        with pytest.raises(InternalError, match="task persistence failed"):
            async for _ in stream:
                pass
        await runner.stopped.wait()
    assert runner.calls == 1
    assert service.task.status.state == a2a.TASK_STATE_SUBMITTED
    assert not service.receipts


async def test_cancellation_waits_for_native_cleanup(runtime):
    service, _, runner, handler = runtime
    runner.cleanup_release.clear()
    stream = handler.on_message_send_stream(send("cancel"), ServerCallContext())
    async with asyncio.timeout(5):
        await anext(stream)
        canceled = asyncio.create_task(handler.on_cancel_task(a2a.CancelTaskRequest(id="task"), ServerCallContext()))
        try:
            await runner.cleanup_started.wait()
            await asyncio.sleep(0.05)
            assert not service.settlements
        finally:
            runner.cleanup_release.set()
        result = await canceled
        assert result.status.state == a2a.TASK_STATE_CANCELED
        await eventually(lambda: bool(service.settlements))
        assert runner.stopped.is_set()
    await stream.aclose()


async def test_disconnected_observer_and_waiting_continuation(runtime):
    service, store, runner, handler = runtime
    stream = handler.on_message_send_stream(send("initial"), ServerCallContext())
    async with asyncio.timeout(5):
        await anext(stream)
    await stream.aclose()
    runner.release.set()
    await eventually(lambda: service.task.status.state == a2a.TASK_STATE_INPUT_REQUIRED)
    assert runner.calls == 1
    assert [message.message_id for message in service.task.history] == ["initial"]
    replay = await handler.on_message_send(send("initial"), ServerCallContext())
    assert replay.status.state == a2a.TASK_STATE_INPUT_REQUIRED
    assert runner.calls == 1
    result = await handler.on_message_send(send("reply", "task"), ServerCallContext())
    assert result.status.state == a2a.TASK_STATE_COMPLETED
    assert runner.calls == 2
    assert [message.message_id for message in service.task.history] == ["initial", "output-1", "reply"]
    replay = await handler.on_message_send(send("reply", "task"), ServerCallContext())
    assert replay.status.state == a2a.TASK_STATE_COMPLETED
    assert runner.calls == 2


async def test_cancel_parked_task(runtime):
    service, _, runner, handler = runtime
    runner.release.set()
    result = await handler.on_message_send(send("park"), ServerCallContext())
    assert result.status.state == a2a.TASK_STATE_INPUT_REQUIRED
    async with asyncio.timeout(5):
        canceled = await handler.on_cancel_task(a2a.CancelTaskRequest(id="task"), ServerCallContext())
    assert canceled.status.state == a2a.TASK_STATE_CANCELED
    assert service.task.status.state == a2a.TASK_STATE_CANCELED
    assert service.settlements[-1] == service.version
    assert runner.calls == 1


async def test_slow_observer_does_not_block_persistence(runtime):
    service, store, runner, handler = runtime
    runner.progress_events = 4 * DEFAULT_MAX_QUEUE_SIZE
    stream = handler.on_message_send_stream(send("slow-observer"), ServerCallContext())
    async with asyncio.timeout(30):
        await anext(stream)
        runner.release.set()
        # Keep the observer attached without reading until every queue would
        # have filled. Persistence and native settlement must still finish.
        await eventually(lambda: bool(service.settlements), timeout=25)
        task = await store.get("task", ServerCallContext())
        assert task.status.state == a2a.TASK_STATE_INPUT_REQUIRED
        assert len(service.receipts) == runner.progress_events + 2
        assert service.settlements[-1] == service.version
        with pytest.raises(StopAsyncIteration):
            await anext(stream)
    await stream.aclose()


async def test_cancel_before_admission_response(runtime):
    service, _, runner, handler = runtime
    service.admission_release.clear()
    pending = asyncio.create_task(handler.on_message_send(send("delayed"), ServerCallContext()))
    async with asyncio.timeout(5):
        await service.admission_committed.wait()
        try:
            canceled = await handler.on_cancel_task(a2a.CancelTaskRequest(id="task"), ServerCallContext())
            assert canceled.status.state == a2a.TASK_STATE_CANCELED
        finally:
            service.admission_release.set()
        with pytest.raises(InvalidParamsError, match="task changed after admission"):
            await pending
    assert runner.calls == 0
    assert service.settlements[-1] == service.version


async def test_observer_read_does_not_advance_writer_version(runtime):
    service, store, _, _ = runtime
    writer = ServerCallContext()
    await store.admit(send("initial"), writer)
    task = a2a.Task()
    task.CopyFrom(service.task)
    task.status.state = a2a.TASK_STATE_WORKING
    await store.save(task, writer)
    stale = ServerCallContext()
    stale_task = await store.get("task", stale)
    task.status.state = a2a.TASK_STATE_COMPLETED
    await store.save(task, writer)
    await store.get("task", ServerCallContext())
    stale_task.status.state = a2a.TASK_STATE_FAILED
    with pytest.raises(grpc.aio.AioRpcError) as error:
        await store.save(stale_task, stale)
    assert error.value.code() == grpc.StatusCode.ABORTED
    assert service.task.status.state == a2a.TASK_STATE_COMPLETED
