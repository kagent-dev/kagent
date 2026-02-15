"""Tests for the executor's two-round tool confirmation flow.

Verifies:
1. Confirmation events (adk_request_confirmation) → input_required as final state
2. Approval resume → FunctionResponse with confirmed=True
3. Rejection resume → FunctionResponse with confirmed=False
4. Normal flow without confirmation → completed (regression)
"""

import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from a2a.types import (
    DataPart,
    Message,
    Part,
    Role,
    TaskState,
    TaskStatus,
    TaskStatusUpdateEvent,
    TextPart,
)

from kagent.adk._agent_executor import (
    A2aAgentExecutor,
    REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
)


MODULE = "kagent.adk._agent_executor"


def _make_run_args(**overrides):
    """Minimal run_args dict returned by convert_a2a_request_to_adk_run_args."""
    args = {
        "user_id": "test_user",
        "session_id": "test_session",
        "new_message": MagicMock(),
        "run_config": MagicMock(),
    }
    args.update(overrides)
    return args


def _make_mock_runner(adk_events=None):
    """Mock Runner with controllable async generator output."""
    runner = MagicMock()
    runner.app_name = "test_app"
    runner.close = AsyncMock()
    runner.session_service = MagicMock()
    runner.session_service.get_session = AsyncMock(return_value=MagicMock(id="test_session"))
    runner.session_service.create_session = AsyncMock()
    runner.session_service.append_event = AsyncMock()
    runner._new_invocation_context = MagicMock(return_value=MagicMock())

    if adk_events is not None:

        async def mock_run_async(**kwargs):
            for event in adk_events:
                yield event

        runner.run_async = mock_run_async

    return runner


def _make_context(
    *,
    current_task=None,
    message=None,
    task_id="task_123",
    context_id="ctx_123",
):
    """Minimal mock RequestContext."""
    ctx = MagicMock()
    ctx.task_id = task_id
    ctx.context_id = context_id
    ctx.current_task = current_task
    ctx.call_context = MagicMock()
    ctx.call_context.state = {}
    ctx.call_context.user = MagicMock()
    ctx.call_context.user.user_name = "test_user"
    ctx.message = message or Message(
        message_id="msg_1",
        role=Role.user,
        parts=[Part(TextPart(text="Hello"))],
    )
    return ctx


def _make_adk_event(*, partial=False, invocation_id="inv_1"):
    """Minimal mock ADK Event."""
    event = MagicMock()
    event.partial = partial
    event.invocation_id = invocation_id
    return event


def _make_input_required_task(function_call_id="fc_123"):
    """Mock task in input_required state with confirmation metadata."""
    task = MagicMock()
    task.status = TaskStatus(
        state=TaskState.input_required,
        message=Message(
            message_id="status_msg",
            role=Role.agent,
            parts=[
                Part(
                    DataPart(
                        data={
                            "name": REQUEST_CONFIRMATION_FUNCTION_CALL_NAME,
                            "id": function_call_id,
                        }
                    )
                ),
            ],
        ),
    )
    return task


def _final_status_events(event_queue):
    """Extract final TaskStatusUpdateEvents from the event queue mock."""
    return [
        call.args[0]
        for call in event_queue.enqueue_event.call_args_list
        if isinstance(call.args[0], TaskStatusUpdateEvent) and getattr(call.args[0], "final", False)
    ]


@pytest.mark.asyncio
@patch(f"{MODULE}.convert_event_to_a2a_events")
@patch(f"{MODULE}.convert_a2a_request_to_adk_run_args")
async def test_confirmation_emits_input_required(mock_convert_request, mock_convert_events):
    """Runner yields adk_request_confirmation → executor emits input_required
    as final state (NOT completed)."""

    adk_event = _make_adk_event()

    mock_convert_events.return_value = [
        TaskStatusUpdateEvent(
            task_id="task_123",
            context_id="ctx_123",
            status=TaskStatus(
                state=TaskState.input_required,
                message=Message(
                    message_id="ir_msg",
                    role=Role.agent,
                    parts=[Part(TextPart(text="Confirm action?"))],
                ),
            ),
            final=False,
        )
    ]
    mock_convert_request.return_value = _make_run_args()

    runner = _make_mock_runner(adk_events=[adk_event])
    executor = A2aAgentExecutor(runner=lambda: MagicMock())

    event_queue = MagicMock()
    event_queue.enqueue_event = AsyncMock()

    with patch.object(executor, "_resolve_runner", new_callable=AsyncMock, return_value=runner):
        await executor.execute(_make_context(), event_queue)

    finals = _final_status_events(event_queue)
    assert len(finals) == 1
    assert finals[0].status.state == TaskState.input_required
    assert finals[0].final is True


@pytest.mark.asyncio
@patch(f"{MODULE}.convert_a2a_request_to_adk_run_args")
async def test_approval_resume(mock_convert_request):
    """Task in input_required + approve → FunctionResponse with confirmed=True."""

    mock_convert_request.return_value = _make_run_args()

    runner = _make_mock_runner()
    executor = A2aAgentExecutor(runner=lambda: MagicMock())

    context = _make_context(
        current_task=_make_input_required_task(function_call_id="fc_abc"),
        message=Message(
            message_id="approve_msg",
            role=Role.user,
            parts=[Part(DataPart(data={"decision_type": "approve"}))],
        ),
    )

    event_queue = MagicMock()
    event_queue.enqueue_event = AsyncMock()

    with (
        patch.object(executor, "_resolve_runner", new_callable=AsyncMock, return_value=runner),
        patch.object(executor, "_handle_request", new_callable=AsyncMock) as mock_handle,
    ):
        await executor.execute(context, event_queue)

    mock_handle.assert_called_once()
    run_args = mock_handle.call_args[0][3]
    new_message = run_args["new_message"]

    assert new_message.role == "user"
    assert len(new_message.parts) == 1
    func_resp = new_message.parts[0].function_response
    assert func_resp.name == REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
    assert func_resp.id == "fc_abc"
    assert func_resp.response == {"confirmed": True}


@pytest.mark.asyncio
@patch(f"{MODULE}.convert_a2a_request_to_adk_run_args")
async def test_rejection_resume(mock_convert_request):
    """Task in input_required + deny → FunctionResponse with confirmed=False."""

    mock_convert_request.return_value = _make_run_args()

    runner = _make_mock_runner()
    executor = A2aAgentExecutor(runner=lambda: MagicMock())

    context = _make_context(
        current_task=_make_input_required_task(function_call_id="fc_xyz"),
        message=Message(
            message_id="deny_msg",
            role=Role.user,
            parts=[Part(DataPart(data={"decision_type": "deny"}))],
        ),
    )

    event_queue = MagicMock()
    event_queue.enqueue_event = AsyncMock()

    with (
        patch.object(executor, "_resolve_runner", new_callable=AsyncMock, return_value=runner),
        patch.object(executor, "_handle_request", new_callable=AsyncMock) as mock_handle,
    ):
        await executor.execute(context, event_queue)

    mock_handle.assert_called_once()
    run_args = mock_handle.call_args[0][3]
    func_resp = run_args["new_message"].parts[0].function_response
    assert func_resp.name == REQUEST_CONFIRMATION_FUNCTION_CALL_NAME
    assert func_resp.id == "fc_xyz"
    assert func_resp.response == {"confirmed": False}


@pytest.mark.asyncio
@patch(f"{MODULE}.convert_event_to_a2a_events")
@patch(f"{MODULE}.convert_a2a_request_to_adk_run_args")
async def test_normal_flow_unchanged(mock_convert_request, mock_convert_events):
    """Regular flow without confirmation → completed. No regressions."""

    adk_event = _make_adk_event()

    mock_convert_events.return_value = [
        TaskStatusUpdateEvent(
            task_id="task_123",
            context_id="ctx_123",
            status=TaskStatus(
                state=TaskState.working,
                message=Message(
                    message_id="normal_msg",
                    role=Role.agent,
                    parts=[Part(TextPart(text="All done"))],
                ),
            ),
            final=False,
        )
    ]
    mock_convert_request.return_value = _make_run_args()

    runner = _make_mock_runner(adk_events=[adk_event])
    executor = A2aAgentExecutor(runner=lambda: MagicMock())

    event_queue = MagicMock()
    event_queue.enqueue_event = AsyncMock()

    with patch.object(executor, "_resolve_runner", new_callable=AsyncMock, return_value=runner):
        await executor.execute(_make_context(), event_queue)

    finals = _final_status_events(event_queue)
    assert len(finals) == 1
    assert finals[0].status.state == TaskState.completed
    assert finals[0].final is True
