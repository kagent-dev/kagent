"""Runtime A2A persistence through the instance-scoped controller TaskStore."""

import asyncio
import os
from pathlib import Path
from typing import AsyncIterator, cast
from uuid import UUID

import grpc
from a2a.server.agent_execution import AgentExecutor
from a2a.server.context import ServerCallContext
from a2a.server.request_handlers import DefaultRequestHandlerV2
from a2a.server.request_handlers.request_handler import validate_request_params
from a2a.server.tasks import TaskStore
from a2a.types import a2a_pb2
from a2a.utils.errors import InternalError, InvalidParamsError, UnsupportedOperationError
from kagent.api.v1alpha1 import task_store_pb2

from kagent.core._grpc import AsyncControllerClient

_VERSION = "kagent.task_store.versions"
_PERSISTED = "kagent.task_store.persisted"
_NATIVE_SETTLED = "kagent.task_store.native_settled"
_PRODUCER = "kagent.task_store.producer"
_FAILED_SAVE = "kagent.task_store.failed_save"
_IDENTITY_PATH = Path("/run/kagent/identity/name")


class KAgentTaskStore(TaskStore):
    """Persist SDK tasks over gRPC with request-local optimistic versions."""

    def __init__(self, client: AsyncControllerClient, identity_path: Path = _IDENTITY_PATH) -> None:
        self.client = client
        self.identity_path = identity_path
        self._executions: dict[str, asyncio.Event] = {}
        self._execution_lock = asyncio.Lock()

    async def _instance_id(self) -> str:
        name = (await asyncio.to_thread(self.identity_path.read_text)).strip()
        if not name.startswith("ai-"):
            raise InternalError("unexpected runtime actor name")
        return str(UUID(name.removeprefix("ai-")))

    async def _call(self, method, request):
        # Substrate replaces the placeholder on egress. Public user credentials
        # never authorize a background save, and the runtime never holds a JWT.
        metadata = (("authorization", "Bearer substrate-actor"),)
        if os.environ.get("KAGENT_INSECURE_TASK_STORE_AUTH") == "true":
            identity = []
            for field in ("atespace", "name", "uid"):
                identity.append((await asyncio.to_thread((self.identity_path.parent / field).read_text)).strip())
            metadata = (("x-kagent-insecure-runtime-identity", "/".join(identity)),)
        for attempt in range(4):
            try:
                return await method(
                    request,
                    timeout=self.client.timeout,
                    metadata=metadata,
                )
            except grpc.aio.AioRpcError as error:
                if error.code() not in (grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.DEADLINE_EXCEEDED) or attempt == 3:
                    raise
                await asyncio.sleep(0.1 * 2**attempt)
        raise AssertionError("unreachable")

    async def save(self, task: a2a_pb2.Task, context: ServerCallContext) -> None:
        if failure := context.state.get(_FAILED_SAVE):
            # Once a save is uncertain, the SDK must not replace that mutation
            # with a synthetic FAILED update at the same expected version.
            raise InternalError("task persistence failed") from failure
        versions = self._versions(context)
        if task.id not in versions:
            await self.get(task.id, context)
        version = versions[task.id]
        # Copy before awaiting: the SDK mutates its cached protobuf task in place.
        if version == 0:
            request = task_store_pb2.TaskStoreServiceCreateTaskRequest(task=task)
            method = self.client.task_store_service.CreateTask
        else:
            request = task_store_pb2.TaskStoreServiceUpdateTaskRequest(task=task, expected_version=version)
            method = self.client.task_store_service.UpdateTask
        request.agent_instance_id = await self._instance_id()
        try:
            result = await self._call(method, request)
        except BaseException as failure:
            context.state[_FAILED_SAVE] = failure
            # The SDK closes its event queue on a store error but awaits the
            # producer. Stop the native runner too: it must not keep issuing
            # tool work after persistence becomes unavailable or conflicts.
            producer = context.state.get(_PRODUCER)
            if producer is not None and producer is not asyncio.current_task():
                producer.cancel()
            raise
        self._versions(context)[task.id] = result.version
        if task.status.state in (a2a_pb2.TASK_STATE_SUBMITTED, a2a_pb2.TASK_STATE_WORKING):
            if persisted := context.state.get(_PERSISTED):
                persisted.set()
        if context.state.get(_NATIVE_SETTLED) and task.status.state in _BOUNDARY_STATES:
            # Cancellation may save its boundary while the original execution
            # is still unwinding. Keep it unpublished until native cleanup ends.
            if finished := self._executions.get(task.id):
                await finished.wait()
            await self._call(
                self.client.task_store_service.SettleTask,
                task_store_pb2.TaskStoreServiceSettleTaskRequest(
                    agent_instance_id=request.agent_instance_id, task_id=task.id, version=result.version
                ),
            )

    async def get(self, task_id: str, context: ServerCallContext) -> a2a_pb2.Task | None:
        try:
            response = await self._call(
                self.client.task_store_service.GetTask,
                task_store_pb2.TaskStoreServiceGetTaskRequest(
                    agent_instance_id=await self._instance_id(), task_id=task_id
                ),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.NOT_FOUND:
                self._versions(context).setdefault(task_id, 0)
                return None
            raise
        self._versions(context).setdefault(task_id, response.stored.version)
        return response.stored.task

    async def list(self, params: a2a_pb2.ListTasksRequest, context: ServerCallContext) -> a2a_pb2.ListTasksResponse:
        response = await self._call(
            self.client.task_store_service.ListTasks,
            task_store_pb2.TaskStoreServiceListTasksRequest(
                agent_instance_id=await self._instance_id(), request=params
            ),
        )
        return response.result

    async def delete(self, task_id: str, context: ServerCallContext) -> None:
        raise InvalidParamsError("task retention is managed by the instance API")

    @staticmethod
    def _versions(context: ServerCallContext) -> dict[str, int]:
        return cast(dict[str, int], context.state.setdefault(_VERSION, {}))


class KAgentRequestHandler(DefaultRequestHandlerV2):
    """Use the SDK handler and surface background persistence failures."""

    def __init__(self, *, agent_executor, task_store, **kwargs):
        super().__init__(agent_executor=_SettledExecutor(agent_executor, task_store), task_store=task_store, **kwargs)

    @validate_request_params
    async def on_cancel_task(self, params: a2a_pb2.CancelTaskRequest, context: ServerCallContext):
        result = await super().on_cancel_task(params, context)
        if failure := context.state.get(_FAILED_SAVE):
            raise InternalError("task persistence failed") from failure
        return result

    @validate_request_params
    async def on_message_send(self, params: a2a_pb2.SendMessageRequest, context: ServerCallContext):
        if self.task_store._execution_lock.locked():
            raise UnsupportedOperationError("instance already has active work")
        result = await super().on_message_send(params, context)
        if failure := context.state.get(_FAILED_SAVE):
            raise InternalError("task persistence failed") from failure
        return result

    @validate_request_params
    async def on_message_send_stream(
        self, params: a2a_pb2.SendMessageRequest, context: ServerCallContext
    ) -> AsyncIterator:
        if self.task_store._execution_lock.locked():
            raise UnsupportedOperationError("instance already has active work")
        async for event in super().on_message_send_stream(params, context):
            yield event
        # The pinned SDK can close subscriptions without propagating a failed
        # save when persisting its fallback FAILED event also fails.
        if failure := context.state.get(_FAILED_SAVE):
            raise InternalError("task persistence failed") from failure


_BOUNDARY_STATES = {
    a2a_pb2.TASK_STATE_COMPLETED,
    a2a_pb2.TASK_STATE_CANCELED,
    a2a_pb2.TASK_STATE_FAILED,
    a2a_pb2.TASK_STATE_REJECTED,
    a2a_pb2.TASK_STATE_INPUT_REQUIRED,
    a2a_pb2.TASK_STATE_AUTH_REQUIRED,
}


class _SettledExecutor(AgentExecutor):
    """Delay only the boundary event until the native runner finishes cleanup."""

    def __init__(self, executor: AgentExecutor, store: KAgentTaskStore):
        self.executor = executor
        self.store = store

    async def execute(self, context, events):
        # The SDK queues each task; the lock also serializes different tasks
        # sharing this actor's native state, including simultaneous new sends.
        async with self.store._execution_lock:
            await self._execute(context, events)

    async def _execute(self, context, events):
        finished = asyncio.Event()
        self.store._executions[context.task_id] = finished
        persisted = asyncio.Event()
        state = context.call_context.state
        state[_PERSISTED] = persisted
        state[_NATIVE_SETTLED] = False
        state[_PRODUCER] = asyncio.current_task()
        try:
            # Wait for normal SDK persistence before native side effects. This
            # makes active work visible to checkpoint/lifecycle transactions.
            if context.current_task is None:
                initial = a2a_pb2.Task(
                    id=context.task_id,
                    context_id=context.context_id,
                    status=a2a_pb2.TaskStatus(state=a2a_pb2.TASK_STATE_SUBMITTED),
                    history=[context.message],
                )
            else:
                # SDK updates mutate its cached protobuf in place. The native
                # executor needs the waiting status/message to validate HITL.
                previous = a2a_pb2.Task()
                previous.CopyFrom(context.current_task)
                context.current_task = previous
                initial = a2a_pb2.TaskStatusUpdateEvent(
                    task_id=context.task_id,
                    context_id=context.context_id,
                    status=a2a_pb2.TaskStatus(state=a2a_pb2.TASK_STATE_WORKING),
                )
            await events.enqueue_event(initial)
            await persisted.wait()
            await self._run(self.executor.execute, context, events)
        finally:
            finished.set()
            del self.store._executions[context.task_id]

    async def cancel(self, context, events):
        # Cancellation takes over the SDK's cached writer. Its preflight read
        # can precede an in-flight save from Execute, so obtain the version at
        # the next serialized save instead of retaining that earlier read.
        context.call_context.state.pop(_VERSION, None)
        await self._run(self.executor.cancel, context, events)

    async def _run(self, operation, context, events):
        context.call_context.state[_NATIVE_SETTLED] = False
        context.call_context.state[_PRODUCER] = asyncio.current_task()
        boundary = None

        class BoundaryQueue:
            async def enqueue_event(self, event):
                nonlocal boundary
                if boundary is not None:
                    raise InternalError("runtime emitted an event after its final boundary")
                if (
                    isinstance(event, (a2a_pb2.Task, a2a_pb2.TaskStatusUpdateEvent))
                    and event.status.state in _BOUNDARY_STATES
                ):
                    boundary = type(event)()
                    boundary.CopyFrom(event)
                else:
                    await events.enqueue_event(event)

        try:
            await operation(context, BoundaryQueue())
        finally:
            context.call_context.state[_NATIVE_SETTLED] = True
        if boundary is not None:
            await events.enqueue_event(boundary)
