import json
import logging
from typing import Any, List, Sequence

from autogen_agentchat.base import TaskResult
from autogen_agentchat.messages import (
    BaseChatMessage,
    ChatMessage,
    HandoffMessage,
    MemoryQueryEvent,
    MessageFactory,
    ModelClientStreamingChunkEvent,
    StopMessage,
    TextMessage,
    ToolCallExecutionEvent,
    ToolCallRequestEvent,
    ToolCallSummaryMessage,
)
from fastapi import APIRouter
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

from autogenstudio.datamodel import Response, TeamResult
from autogenstudio.datamodel.types import LLMCallEventMessage
from autogenstudio.teammanager import TeamManager

router = APIRouter()
team_manager = TeamManager()
logger = logging.getLogger(__name__)

message_factory = MessageFactory()


class InvokeTaskRequest(BaseModel):
    task: str
    team_config: dict
    messages: List[dict] | None = None


@router.post("/")
async def invoke(request: InvokeTaskRequest):
    response = Response(message="Task successfully completed", status=True, data=None)
    try:
        previous_messages = _convert_message_config_to_chat_message(request.messages or [])
        task = _prepare_task_with_history(request.task, previous_messages)
        result_message: TeamResult = await team_manager.run(task=task, team_config=request.team_config)
        # remove the previous messages from the result messages
        result_message.task_result.messages = result_message.task_result.messages[len(previous_messages) :]
        formatted_result = format_team_result(result_message)
        response.data = formatted_result
    except Exception as e:
        response.message = str(e)
        response.status = False
    return response


def format_team_result(team_result: TeamResult) -> dict:
    """
    Format the result from TeamResult to a dictionary.
    """
    formatted_result = {
        "task_result": format_task_result(team_result.task_result),
        "usage": team_result.usage,
        "duration": team_result.duration,
    }
    return formatted_result


def format_task_result(task_result: TaskResult) -> dict:
    """
    Format the result from TeamResult to a dictionary.
    """
    formatted_result = {
        "messages": [format_message(message) for message in task_result.messages],
        "stop_reason": task_result.stop_reason,
    }
    return formatted_result


def format_message(message: Any) -> dict:
    """Format message for sse transmission

    Args:
        message: Message to format

    Returns:
        Optional[dict]: Formatted message or None if formatting fails
    """

    try:
        if isinstance(
            message,
            (
                ModelClientStreamingChunkEvent,
                TextMessage,
                StopMessage,
                HandoffMessage,
                ToolCallRequestEvent,
                ToolCallExecutionEvent,
                LLMCallEventMessage,
                MemoryQueryEvent,
                ToolCallSummaryMessage,
            ),
        ):
            return message.model_dump(exclude={"created_at"})

        elif isinstance(message, TeamResult):
            return format_team_result(message)

        return {"type": "unknown", "data": f"received unknown message type {type(message)}"}

    except Exception as e:
        logger.error(f"Message formatting error: {e}")
        return {"type": "error", "data": str(e)}


@router.post("/stream")
async def stream(request: InvokeTaskRequest):
    logger.info(f"Invoking task with streaming: {request.task}")

    async def event_generator():
        try:
            previous_messages = _convert_message_config_to_chat_message(request.messages or [])
            num_previous_messages = len(previous_messages)
            task = _prepare_task_with_history(request.task, previous_messages)
            async for event in team_manager.run_stream(task=task, team_config=request.team_config):
                if num_previous_messages > 0:
                    num_previous_messages -= 1
                    continue
                if isinstance(event, TeamResult):
                    yield f"event: task_result\ndata: {json.dumps(format_message(event))}\n\n"
                else:
                    yield f"event: event\ndata: {json.dumps(format_message(event))}\n\n"
        except Exception as e:
            logger.error(f"Error during SSE stream generation: {e}", exc_info=True)
            error_payload = {"type": "error", "data": {"message": str(e), "details": type(e).__name__}}
            try:
                yield f"data: {json.dumps(error_payload)}\n\n"
            except Exception as yield_err:  # pylint: disable=broad-except
                logger.error(f"Error yielding error message to client: {yield_err}", exc_info=True)

    return StreamingResponse(event_generator(), media_type="text/event-stream")


def _convert_message_config_to_chat_message(raw_messages: list[dict]) -> list[BaseChatMessage]:
    """Convert MessageConfig to appropriate BaseChatMessage type using MessageFactory"""

    messages = []
    for message_config in raw_messages:
        message = message_factory.create(message_config)
        if isinstance(message, BaseChatMessage):
            messages.append(message)

    return messages


def _prepare_task_with_history(
    task: str | BaseChatMessage | Sequence[BaseChatMessage] | None,
    previous_messages: Sequence[BaseChatMessage],
) -> str | BaseChatMessage | Sequence[BaseChatMessage] | None:
    """Combine previous messages with current task for team execution"""
    if not previous_messages:
        return task

    # If we have previous messages, combine them with the current task
    if isinstance(task, str):
        return list(previous_messages) + [TextMessage(source="user", content=task)]
    elif isinstance(task, ChatMessage):
        return list(previous_messages) + [task]
    elif isinstance(task, list):
        return list(previous_messages) + list(task)
    else:
        return list(previous_messages)
