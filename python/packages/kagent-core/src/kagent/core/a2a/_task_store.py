"""Runtime A2A persistence through the instance-scoped controller TaskStore."""

import asyncio
import os
from pathlib import Path
from typing import AsyncIterator, cast
from uuid import UUID, uuid4

import grpc
from a2a.server.agent_execution import AgentExecutor
from a2a.server.context import ServerCallContext
from a2a.server.request_handlers import DefaultRequestHandlerV2
from a2a.server.request_handlers.request_handler import validate_request_params
from a2a.server.tasks import TaskStore
from a2a.types import a2a_pb2
from a2a.utils.errors import InternalError, InvalidParamsError, TaskNotFoundError, UnsupportedOperationError
from a2a.utils.task import apply_history_length, validate_history_length
from kagent.api.v1alpha1 import task_store_pb2

from kagent.core._grpc import AsyncControllerClient

_VERSION = "kagent.task_store.versions"
_SEED = "kagent.task_store.seed"
_INPUT_SAVE = "kagent.task_store.input_save"
_ADMITTED_STATUS = "kagent.task_store.admitted_status"
_NATIVE_SETTLED = "kagent.task_store.native_settled"
_PRODUCER = "kagent.task_store.producer"
_FAILED_SAVE = "kagent.task_store.failed_save"
_IDENTITY_PATH = Path("/run/kagent/identity/name")


class KAgentTaskStore(TaskStore):
    """Store public task data centrally; retain only request-local SDK versions.

    The V2 SDK changes its TaskManager call context for each admitted reply, even
    when it keeps the waiting task in memory. That context carries the admission
    version. Observer reads use their own context and cannot advance a writer.
    """

    def __init__(self, client: AsyncControllerClient, identity_path: Path = _IDENTITY_PATH) -> None:
        self.client = client
        self.identity_path = identity_path
        self._executions: dict[str, asyncio.Event] = {}

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

    async def admit(self, params: a2a_pb2.SendMessageRequest, context: ServerCallContext) -> a2a_pb2.Task | None:
        """Return the replayed task, or prepare a newly admitted SDK execution."""
        validate_history_length(params.configuration)
        try:
            response = await self._call(
                self.client.task_store_service.AdmitMessage,
                task_store_pb2.TaskStoreServiceAdmitMessageRequest(
                    agent_instance_id=await self._instance_id(), admission_id=str(uuid4()), request=params
                ),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.ABORTED:
                raise UnsupportedOperationError(
                    "instance already has active work or a pending lifecycle operation"
                ) from error
            if error.code() in (
                grpc.StatusCode.INVALID_ARGUMENT,
                grpc.StatusCode.ALREADY_EXISTS,
                grpc.StatusCode.FAILED_PRECONDITION,
            ):
                raise InvalidParamsError(error.details()) from error
            if error.code() == grpc.StatusCode.NOT_FOUND:
                raise TaskNotFoundError(error.details()) from error
            raise
        current = response.current
        if not response.admitted:
            return apply_history_length(current.task, params.configuration)
        self._versions(context)[current.task.id] = current.version
        seed = a2a_pb2.Task()
        seed.CopyFrom(response.previous if response.HasField("previous") else current.task)
        context.state[_ADMITTED_STATUS] = current.task.status
        if not response.HasField("previous"):
            # Python appends the admitted message itself when consuming its first
            # event. Start without that message, preserving all earlier history.
            inherited = [message for message in seed.history if message.message_id != params.message.message_id]
            del seed.history[:]
            seed.history.extend(inherited)
        context.state[_SEED] = seed
        for message in current.task.history:
            if message.message_id == params.message.message_id:
                params.message.CopyFrom(message)
                break
        else:
            raise InternalError("admitted task does not contain its input")
        return None

    async def save(self, task: a2a_pb2.Task, context: ServerCallContext) -> None:
        if failure := context.state.get(_FAILED_SAVE):
            # Once a save is uncertain, the SDK must not replace that mutation
            # with a synthetic FAILED update at the same expected version.
            raise InternalError("task persistence failed") from failure
        version = self._versions(context).get(task.id)
        if version is None:
            raise InvalidParamsError("task must be admitted or loaded before saving")
        if task == context.state.get(_INPUT_SAVE):
            context.state.pop(_INPUT_SAVE)
            # Admission already persisted this input. Advance the cached task
            # too: an artifact may arrive before the next status event, and must
            # not inherit the previous turn's waiting state.
            task.status.CopyFrom(context.state.pop(_ADMITTED_STATUS))
            context.state.pop(_SEED, None)
            return
        # Copy before awaiting: the SDK mutates its cached protobuf task in place.
        request = task_store_pb2.TaskStoreServiceUpdateTaskRequest(task=task, expected_version=version)
        request.agent_instance_id = await self._instance_id()
        try:
            result = await self._call(self.client.task_store_service.UpdateTask, request)
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
        context.state.pop(_SEED, None)
        context.state.pop(_INPUT_SAVE, None)
        context.state.pop(_ADMITTED_STATUS, None)
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
                return None
            raise
        seed = context.state.get(_SEED)
        if seed is not None and seed.id == task_id:
            if self._versions(context)[task_id] != response.stored.version:
                raise InvalidParamsError("task changed after admission")
            result = a2a_pb2.Task()
            result.CopyFrom(seed)
            return result
        self._versions(context)[task_id] = response.stored.version
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
    """Admit input before the SDK can mutate task history or invoke the runner."""

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
        replay = await cast(KAgentTaskStore, self.task_store).admit(params, context)
        if replay is not None:
            return replay
        result = await super().on_message_send(params, context)
        if failure := context.state.get(_FAILED_SAVE):
            raise InternalError("task persistence failed") from failure
        return result

    @validate_request_params
    async def on_message_send_stream(
        self, params: a2a_pb2.SendMessageRequest, context: ServerCallContext
    ) -> AsyncIterator:
        replay = await cast(KAgentTaskStore, self.task_store).admit(params, context)
        if replay is not None:
            yield replay
            return
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
        if context.current_task is not None:
            # The SDK may reuse its pre-pause task, whose history need not have
            # the store's canonical representation. Match its input-only save
            # against that exact cached task, before native code can mutate it.
            input_save = a2a_pb2.Task()
            input_save.CopyFrom(context.current_task)
            if input_save.status.HasField("message"):
                input_save.history.append(input_save.status.message)
                input_save.status.ClearField("message")
            input_save.history.append(context.message)
            context.call_context.state[_INPUT_SAVE] = input_save
        finished = asyncio.Event()
        self.store._executions[context.task_id] = finished
        try:
            await self._run(self.executor.execute, context, events)
        finally:
            finished.set()
            del self.store._executions[context.task_id]

    async def cancel(self, context, events):
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
