import os
from typing import Union

from autogen_agentchat.base import TaskResult
from autogen_agentchat.messages import BaseTextChatMessage
from autogen_core import ComponentModel
from fastapi import APIRouter
from pydantic import BaseModel

from autogenstudio.datamodel import Response, TeamResult
from autogenstudio.teammanager import TeamManager

router = APIRouter()
team_manager = TeamManager()


class InvokeTaskRequest(BaseModel):
    task: str
    team_config: dict


@router.post("/")
async def invoke(request: InvokeTaskRequest):
    response = Response(message="Task successfully completed", status=True, data=None)
    try:
        result_message = await team_manager.run(task=request.task, team_config=request.team_config)
        response.data = _format_team_result(result_message)
    except Exception as e:
        response.message = str(e)
        response.status = False
    return response


def _format_team_result(team_result: TeamResult) -> dict:
    """
    Format the result from TeamResult to a dictionary.
    """
    formatted_result = {
        "task_result": _format_task_result(team_result.task_result),
        "usage": team_result.usage,
        "duration": team_result.duration,
    }
    return formatted_result


def _format_task_result(task_result: TaskResult) -> dict:
    """
    Format the result from TeamResult to a dictionary.
    """
    formatted_result = {
        "messages": [_format_message(message) for message in task_result.messages],
        "stop_reason": task_result.stop_reason,
    }
    return formatted_result


def _format_message(message: BaseTextChatMessage) -> dict:
    """
    Format the message to a dictionary.
    """
    return {
        "source": message.source,
        "models_usage": message.models_usage,
        "metadata": message.metadata,
        "content": message.content,
    }
