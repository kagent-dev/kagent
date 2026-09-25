import asyncio
from unittest.mock import AsyncMock, MagicMock

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.server.events.in_memory_queue_manager import InMemoryQueueManager
from a2a.server.request_handlers.default_request_handler import DefaultRequestHandler
from a2a.server.tasks.inmemory_task_store import InMemoryTaskStore
from a2a.types import (
    Message,
    MessageSendParams,
    Part,
    Role,
    Task,
    TaskIdParams,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)
from google.adk.runners import Runner

from kagent.adk._agent_executor import A2aAgentExecutor

TASK_ID = "task-2096"
CONTEXT_ID = "context-2096"


def _executor_without_runtime_access():
    runner_factory = MagicMock(name="runner_factory")
    executor_task_store = MagicMock(name="executor_task_store")
    executor = A2aAgentExecutor(runner=runner_factory, task_store=executor_task_store)
    executor._resolve_runner = AsyncMock(name="resolve_runner")
    executor._prepare_session = AsyncMock(name="prepare_session")
    return executor, runner_factory, executor_task_store


def _working_task():
    return Task(
        id=TASK_ID,
        contextId=CONTEXT_ID,
        status=TaskStatus(state=TaskState.working),
    )


async def _start_blocked_execution(execution_queue):
    executor_task_store = MagicMock(name="executor_task_store")
    executor = A2aAgentExecutor(runner=MagicMock(), task_store=executor_task_store)
    runner = MagicMock(spec=Runner)
    runner.close = AsyncMock()
    executor._resolve_runner = AsyncMock(return_value=runner)
    request_started = asyncio.Event()

    async def blocked_handle_request(*_args):
        request_started.set()
        await asyncio.Event().wait()

    executor._handle_request = AsyncMock(side_effect=blocked_handle_request)
    message = Message(
        role=Role.user,
        message_id="message-2096",
        task_id=TASK_ID,
        context_id=CONTEXT_ID,
        parts=[Part(TextPart(text="block until canceled"))],
    )
    execution_context = RequestContext(
        MessageSendParams(message=message),
        task_id=TASK_ID,
        context_id=CONTEXT_ID,
        task=_working_task(),
    )
    producer_task = asyncio.create_task(executor.execute(execution_context, execution_queue))
    await request_started.wait()
    return executor, producer_task, runner, executor_task_store


async def _drain_events(queue):
    events = []
    while True:
        try:
            events.append(await queue.dequeue_event(no_wait=True))
        except asyncio.QueueEmpty:
            return events


@pytest.mark.asyncio
async def test_cancel_publishes_canonical_event_without_runtime_or_store_access():
    executor, runner_factory, executor_task_store = _executor_without_runtime_access()
    queue = EventQueue()

    await executor.cancel(
        RequestContext(task_id=TASK_ID, context_id=CONTEXT_ID),
        queue,
    )

    event = await queue.dequeue_event(no_wait=True)
    assert isinstance(event, TaskStatusUpdateEvent)
    assert event.task_id == TASK_ID
    assert event.context_id == CONTEXT_ID
    assert event.status.state == TaskState.canceled
    assert event.status.timestamp is not None
    assert event.status.message is None
    assert event.final is True
    with pytest.raises(asyncio.QueueEmpty):
        await queue.dequeue_event(no_wait=True)

    runner_factory.assert_not_called()
    executor._resolve_runner.assert_not_awaited()
    executor._prepare_session.assert_not_awaited()
    assert executor_task_store.mock_calls == []


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("task_id", "context_id"),
    [(None, CONTEXT_ID), (TASK_ID, None)],
)
async def test_cancel_rejects_missing_task_coordinates(task_id, context_id):
    executor, runner_factory, executor_task_store = _executor_without_runtime_access()
    queue = EventQueue()

    with pytest.raises(ValueError, match="task and context IDs"):
        await executor.cancel(
            RequestContext(task_id=task_id, context_id=context_id),
            queue,
        )

    with pytest.raises(asyncio.QueueEmpty):
        await queue.dequeue_event(no_wait=True)
    runner_factory.assert_not_called()
    executor._resolve_runner.assert_not_awaited()
    executor._prepare_session.assert_not_awaited()
    assert executor_task_store.mock_calls == []


class _RecordingEventQueue(EventQueue):
    def __init__(self, ordering):
        super().__init__()
        self.ordering = ordering

    async def enqueue_event(self, event):
        await super().enqueue_event(event)
        if isinstance(event, TaskStatusUpdateEvent) and event.status.state == TaskState.canceled:
            self.ordering.append("canceled-published")


class _QueueManager:
    def __init__(self, queue):
        self.queue = queue

    async def tap(self, task_id):
        assert task_id == TASK_ID
        return self.queue


@pytest.mark.asyncio
async def test_handler_publishes_cancellation_before_canceling_producer():
    ordering = []
    queue = _RecordingEventQueue(ordering)
    store = InMemoryTaskStore()
    await store.save(_working_task())
    executor, runner_factory, executor_task_store = _executor_without_runtime_access()
    handler = DefaultRequestHandler(executor, store, _QueueManager(queue))

    producer_started = asyncio.Event()
    producer_canceled = asyncio.Event()

    async def producer():
        producer_started.set()
        try:
            await asyncio.Event().wait()
        except asyncio.CancelledError:
            ordering.append("producer-canceled")
            producer_canceled.set()
            raise

    producer_task = asyncio.create_task(producer())
    await producer_started.wait()
    handler._running_agents[TASK_ID] = producer_task

    try:
        result = await handler.on_cancel_task(TaskIdParams(id=TASK_ID))
        await producer_canceled.wait()

        assert ordering == ["canceled-published", "producer-canceled"]
        assert producer_task.cancelling() == 1
        assert result is not None
        assert result.id == TASK_ID
        assert result.context_id == CONTEXT_ID
        assert result.status.state == TaskState.canceled
        persisted = await store.get(TASK_ID)
        assert persisted is not None
        assert persisted.id == TASK_ID
        assert persisted.context_id == CONTEXT_ID
        assert persisted.status.state == TaskState.canceled
        runner_factory.assert_not_called()
        executor._resolve_runner.assert_not_awaited()
        executor._prepare_session.assert_not_awaited()
        assert executor_task_store.mock_calls == []
    finally:
        if not producer_task.done():
            producer_task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await producer_task


@pytest.mark.asyncio
async def test_explicit_cancel_does_not_publish_failed_from_real_execution_path():
    store = InMemoryTaskStore()
    await store.save(_working_task())
    queue_manager = InMemoryQueueManager()
    execution_queue = await queue_manager.create_or_tap(TASK_ID)
    executor, producer_task, runner, executor_task_store = await _start_blocked_execution(execution_queue)
    handler = DefaultRequestHandler(executor, store, queue_manager)
    handler._running_agents[TASK_ID] = producer_task

    result = await handler.on_cancel_task(TaskIdParams(id=TASK_ID))
    await producer_task

    assert result is not None
    assert result.id == TASK_ID
    assert result.context_id == CONTEXT_ID
    assert result.status.state == TaskState.canceled
    persisted = await store.get(TASK_ID)
    assert persisted is not None
    assert persisted.status.state == TaskState.canceled
    remaining_events = await _drain_events(execution_queue)
    assert not any(
        isinstance(event, TaskStatusUpdateEvent) and event.status.state == TaskState.failed
        for event in remaining_events
    )
    executor._resolve_runner.assert_awaited_once()
    executor._handle_request.assert_awaited_once()
    runner.close.assert_awaited_once()
    assert executor_task_store.mock_calls == []


@pytest.mark.asyncio
async def test_unrelated_execution_cancellation_still_publishes_failed():
    execution_queue = EventQueue()
    executor, producer_task, runner, executor_task_store = await _start_blocked_execution(execution_queue)

    producer_task.cancel()
    await producer_task

    events = await _drain_events(execution_queue)
    assert len(events) == 1
    event = events[0]
    assert isinstance(event, TaskStatusUpdateEvent)
    assert event.task_id == TASK_ID
    assert event.context_id == CONTEXT_ID
    assert event.status.state == TaskState.failed
    assert event.final is True
    executor._resolve_runner.assert_awaited_once()
    executor._handle_request.assert_awaited_once()
    runner.close.assert_awaited_once()
    assert executor_task_store.mock_calls == []


@pytest.mark.asyncio
async def test_handler_cancels_and_persists_when_queue_tap_misses():
    store = InMemoryTaskStore()
    await store.save(_working_task())
    executor, runner_factory, executor_task_store = _executor_without_runtime_access()
    handler = DefaultRequestHandler(
        executor,
        store,
        InMemoryQueueManager(),
    )

    result = await handler.on_cancel_task(TaskIdParams(id=TASK_ID))

    assert result is not None
    assert result.id == TASK_ID
    assert result.context_id == CONTEXT_ID
    assert result.status.state == TaskState.canceled
    persisted = await store.get(TASK_ID)
    assert persisted is not None
    assert persisted.id == TASK_ID
    assert persisted.context_id == CONTEXT_ID
    assert persisted.status.state == TaskState.canceled
    runner_factory.assert_not_called()
    executor._resolve_runner.assert_not_awaited()
    executor._prepare_session.assert_not_awaited()
    assert executor_task_store.mock_calls == []
