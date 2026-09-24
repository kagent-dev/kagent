"""Run by Go's TestRuntimeTaskStoreThroughGRPC with KAGENT_TEST_PYTHON set."""

import asyncio
import json
import os
from pathlib import Path
from time import perf_counter
from uuid import uuid4

import grpc
from a2a.server.context import ServerCallContext
from a2a.types import a2a_pb2 as a2a
from a2a.utils.errors import UnsupportedOperationError
from kagent.api.v1alpha1 import task_store_pb2 as storage
from test_task_store import Runner
from test_task_store import send as make_send

from kagent.core._grpc import AsyncControllerClient
from kagent.core.a2a._task_store import KAgentRequestHandler, KAgentTaskStore


def send(message_id, task_id=""):
    request = make_send(message_id, task_id)
    request.message.context_id = os.environ["KAGENT_TASKSTORE_TEST_CONTEXT"]
    return request


class TestEgressInjection(grpc.aio.UnaryUnaryClientInterceptor):
    """Test-only replacement for Substrate's mTLS-derived actor JWT injection."""

    async def intercept_unary_unary(self, continuation, details, request):
        metadata = [
            (key, "Bearer " + os.environ["KAGENT_TASKSTORE_TEST_TOKEN"] if key == "authorization" else value)
            for key, value in details.metadata
        ]
        return await continuation(details._replace(metadata=metadata), request)


async def wait_public(store, task_id, state):
    async with asyncio.timeout(10):
        while True:
            result = await store.list(a2a.ListTasksRequest(page_size=100, include_artifacts=True), ServerCallContext())
            for task in result.tasks:
                if task.id == task_id and task.status.state == state:
                    return task
            await asyncio.sleep(0.01)


async def measure_persistence(store):
    """Optional loopback cost probe, independent of native/model latency."""
    instance_id = await store._instance_id()
    service = store.client.task_store_service
    task = a2a.Task(
        id=str(uuid4()),
        context_id=os.environ["KAGENT_TASKSTORE_TEST_CONTEXT"],
        status=a2a.TaskStatus(state=a2a.TASK_STATE_SUBMITTED),
        history=[send(str(uuid4())).message],
    )
    created = await store._call(
        service.CreateTask, storage.TaskStoreServiceCreateTaskRequest(agent_instance_id=instance_id, task=task)
    )
    version = created.version
    task.status.state = a2a.TASK_STATE_WORKING
    measurements = []
    for size in (256, 32 * 1024, 256 * 1024):
        del task.artifacts[:]
        task.artifacts.append(a2a.Artifact(artifact_id="sized-output", parts=[a2a.Part(text="x" * size)]))
        saves, reads = [], []
        for _ in range(50):
            task.status.timestamp.GetCurrentTime()
            request = storage.TaskStoreServiceUpdateTaskRequest(
                agent_instance_id=instance_id, task=task, expected_version=version
            )
            started = perf_counter()
            saved = await store._call(service.UpdateTask, request)
            saves.append((perf_counter() - started) * 1000)
            version = saved.version
            started = perf_counter()
            await store.get(task.id, ServerCallContext())
            reads.append((perf_counter() - started) * 1000)
        for operation, samples in (("snapshot save", saves), ("private read", reads)):
            samples.sort()
            measurements.append(
                {
                    "operation": operation,
                    "artifact_bytes": size,
                    "request_bytes": request.ByteSize(),
                    "samples": len(samples),
                    "p50_ms": round(samples[24], 2),
                    "p95_ms": round(samples[47], 2),
                    "p99_ms": round(samples[49], 2),
                }
            )
    task.status.state = a2a.TASK_STATE_COMPLETED
    saved = await store._call(
        service.UpdateTask,
        storage.TaskStoreServiceUpdateTaskRequest(agent_instance_id=instance_id, task=task, expected_version=version),
    )
    await store._call(
        service.SettleTask,
        storage.TaskStoreServiceSettleTaskRequest(
            agent_instance_id=instance_id, task_id=task.id, version=saved.version
        ),
    )
    await wait_public(store, task.id, a2a.TASK_STATE_COMPLETED)
    print(json.dumps(measurements))  # noqa: T201 - command-line measurement report


async def main():
    async with grpc.aio.insecure_channel(
        os.environ["KAGENT_TASKSTORE_TEST_ENDPOINT"], interceptors=[TestEgressInjection()]
    ) as channel:
        client = AsyncControllerClient(channel=channel)
        store = KAgentTaskStore(client, Path(os.environ["KAGENT_TASKSTORE_TEST_IDENTITY"]))
        runner = Runner()
        runner.rich_history = True
        handler = KAgentRequestHandler(
            agent_executor=runner,
            task_store=store,
            agent_card=a2a.AgentCard(capabilities=a2a.AgentCapabilities(streaming=True)),
        )
        try:
            initial_id, reply_id = str(uuid4()), str(uuid4())
            stream = handler.on_message_send_stream(send(initial_id), ServerCallContext())
            event = await anext(stream)
            task_id = event.id if isinstance(event, a2a.Task) else event.task_id
            try:
                await handler.on_message_send(send(str(uuid4())), ServerCallContext())
            except UnsupportedOperationError:
                pass
            else:
                raise AssertionError("busy execution must return an A2A precondition error")
            await stream.aclose()
            runner.release.set()
            waiting = await wait_public(store, task_id, a2a.TASK_STATE_INPUT_REQUIRED)
            assert waiting.status.message.parts[0].text == "continue?"
            replay = await store.get(task_id, ServerCallContext())
            assert replay.id == task_id and runner.calls == 1
            async with asyncio.timeout(5):
                completed = await handler.on_message_send(send(reply_id, task_id), ServerCallContext())
            assert completed.status.state == a2a.TASK_STATE_COMPLETED
            published = await wait_public(store, task_id, a2a.TASK_STATE_COMPLETED)
            history_ids = [message.message_id for message in published.history]
            assert history_ids.count(initial_id) == 1
            assert history_ids.count(reply_id) == 1
            assert history_ids.count("progress") == 1
            assert "output-1" in history_ids
            assert runner.calls == 2
            replay = await store.get(task_id, ServerCallContext())
            assert replay.id == task_id and runner.calls == 2
            runner.release.clear()
            stream = handler.on_message_send_stream(send(str(uuid4())), ServerCallContext())
            event = await anext(stream)
            cancel_id = event.id if isinstance(event, a2a.Task) else event.task_id
            async with asyncio.timeout(5):
                canceled = await handler.on_cancel_task(a2a.CancelTaskRequest(id=cancel_id), ServerCallContext())
            assert canceled.status.state == a2a.TASK_STATE_CANCELED
            await wait_public(store, cancel_id, a2a.TASK_STATE_CANCELED)
            await stream.aclose()
            print(  # noqa: T201 - command-line conformance report
                "Python SDK -> authenticated gRPC -> PostgreSQL: disconnect, continuation, cancellation, reads, history and lost responses passed"
            )
            if os.environ.get("KAGENT_TASKSTORE_BENCHMARK") == "1":
                await measure_persistence(store)
        finally:
            await handler.aclose()
            await client.close()


if __name__ == "__main__":
    asyncio.run(main())
