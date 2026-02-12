import json
from unittest.mock import AsyncMock, Mock, patch

import pytest
from a2a.server.agent_execution.context import RequestContext
from a2a.server.events.event_queue import EventQueue
from a2a.types import (
    Message,
    TaskArtifactUpdateEvent,
    TaskState,
    TaskStatusUpdateEvent,
)
from dapr_agents import DurableAgent
from kagent.dapr._durable import DaprDurableAgentExecutor


@pytest.fixture
def mock_durable_agent():
    agent = Mock(spec=DurableAgent)
    agent.start = Mock()
    agent.agent_workflow = Mock()
    return agent


@pytest.fixture
def mock_wf_client():
    return Mock()


@pytest.fixture
def executor(mock_durable_agent, mock_wf_client):
    with patch("kagent.dapr._durable.wf.DaprWorkflowClient", return_value=mock_wf_client):
        ex = DaprDurableAgentExecutor(durable_agent=mock_durable_agent, app_name="test-app")
    return ex


@pytest.fixture
def event_queue():
    eq = Mock(spec=EventQueue)
    eq.enqueue_event = AsyncMock()
    return eq


@pytest.fixture
def context():
    ctx = Mock(spec=RequestContext)
    ctx.task_id = "task-123"
    ctx.context_id = "ctx-456"
    ctx.message = Mock(spec=Message)
    ctx.current_task = None
    ctx.call_context = None
    ctx.get_user_input = Mock(return_value="Hello agent")
    return ctx


@pytest.mark.asyncio
async def test_execute_success(executor, event_queue, context, mock_wf_client):
    """Workflow completes with a JSON string result."""
    result_state = Mock()
    result_state.serialized_output = json.dumps("The answer is 42")
    mock_wf_client.schedule_new_workflow = Mock(return_value="wf-instance-1")
    mock_wf_client.wait_for_workflow_completion = Mock(return_value=result_state)

    await executor.execute(context, event_queue)

    # Should emit: submitted, working, artifact, completed
    assert event_queue.enqueue_event.call_count == 4

    submitted_event = event_queue.enqueue_event.call_args_list[0][0][0]
    assert isinstance(submitted_event, TaskStatusUpdateEvent)
    assert submitted_event.status.state == TaskState.submitted

    working_event = event_queue.enqueue_event.call_args_list[1][0][0]
    assert isinstance(working_event, TaskStatusUpdateEvent)
    assert working_event.status.state == TaskState.working

    artifact_event = event_queue.enqueue_event.call_args_list[2][0][0]
    assert isinstance(artifact_event, TaskArtifactUpdateEvent)
    assert artifact_event.artifact.parts[0].root.text == "The answer is 42"

    completed_event = event_queue.enqueue_event.call_args_list[3][0][0]
    assert isinstance(completed_event, TaskStatusUpdateEvent)
    assert completed_event.status.state == TaskState.completed
    assert completed_event.final is True


@pytest.mark.asyncio
async def test_execute_success_dict_result(executor, event_queue, context, mock_wf_client):
    """Workflow returns dict with 'content' key."""
    result_state = Mock()
    result_state.serialized_output = json.dumps({"content": "Dict result"})
    mock_wf_client.schedule_new_workflow = Mock(return_value="wf-instance-2")
    mock_wf_client.wait_for_workflow_completion = Mock(return_value=result_state)

    await executor.execute(context, event_queue)

    artifact_event = event_queue.enqueue_event.call_args_list[2][0][0]
    assert isinstance(artifact_event, TaskArtifactUpdateEvent)
    assert artifact_event.artifact.parts[0].root.text == "Dict result"


@pytest.mark.asyncio
async def test_execute_workflow_failure(executor, event_queue, context, mock_wf_client):
    """Workflow raises an exception."""
    mock_wf_client.schedule_new_workflow = Mock(side_effect=RuntimeError("Workflow crashed"))

    await executor.execute(context, event_queue)

    # Should emit: submitted, working, failed
    assert event_queue.enqueue_event.call_count == 3

    failed_event = event_queue.enqueue_event.call_args_list[2][0][0]
    assert isinstance(failed_event, TaskStatusUpdateEvent)
    assert failed_event.status.state == TaskState.failed
    assert "Workflow crashed" in failed_event.status.message.parts[0].root.text


@pytest.mark.asyncio
async def test_execute_no_message(executor, event_queue):
    """context.message is None raises ValueError."""
    ctx = Mock(spec=RequestContext)
    ctx.message = None

    with pytest.raises(ValueError, match="must have a message"):
        await executor.execute(ctx, event_queue)


@pytest.mark.asyncio
async def test_cancel_not_implemented(executor, event_queue, context):
    """cancel() raises NotImplementedError."""
    with pytest.raises(NotImplementedError):
        await executor.cancel(context, event_queue)


@pytest.mark.asyncio
async def test_submitted_event_for_new_task(executor, event_queue, context, mock_wf_client):
    """No current_task emits submitted event."""
    context.current_task = None
    result_state = Mock()
    result_state.serialized_output = json.dumps("ok")
    mock_wf_client.schedule_new_workflow = Mock(return_value="wf-1")
    mock_wf_client.wait_for_workflow_completion = Mock(return_value=result_state)

    await executor.execute(context, event_queue)

    submitted_event = event_queue.enqueue_event.call_args_list[0][0][0]
    assert isinstance(submitted_event, TaskStatusUpdateEvent)
    assert submitted_event.status.state == TaskState.submitted


@pytest.mark.asyncio
async def test_no_submitted_event_for_existing_task(executor, event_queue, context, mock_wf_client):
    """current_task exists skips submitted event."""
    context.current_task = Mock()  # existing task
    result_state = Mock()
    result_state.serialized_output = json.dumps("ok")
    mock_wf_client.schedule_new_workflow = Mock(return_value="wf-1")
    mock_wf_client.wait_for_workflow_completion = Mock(return_value=result_state)

    await executor.execute(context, event_queue)

    # Should emit: working, artifact, completed (no submitted)
    assert event_queue.enqueue_event.call_count == 3

    first_event = event_queue.enqueue_event.call_args_list[0][0][0]
    assert isinstance(first_event, TaskStatusUpdateEvent)
    assert first_event.status.state == TaskState.working
