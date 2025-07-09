# from dataclasses import Field
from datetime import datetime
from typing import Any, Dict, List, Literal, Optional, Sequence

from autogen_agentchat.base import TaskResult
from autogen_agentchat.messages import ChatMessage, TextMessage
from autogen_core import ComponentModel
from autogen_core.models import UserMessage
from autogen_ext.models.openai import OpenAIChatCompletionClient
from pydantic import BaseModel, ConfigDict, SecretStr


class MessageConfig(BaseModel):
    source: str
    content: str | ChatMessage | Sequence[ChatMessage] | None
    message_type: Optional[str] = "text"


class TeamResult(BaseModel):
    task_result: TaskResult
    usage: str
    duration: float


class LLMCallEventMessage(TextMessage):
    source: str = "llm_call_event"

    def to_text(self) -> str:
        return self.content

    def to_model_text(self) -> str:
        return self.content

    def to_model_message(self) -> UserMessage:
        raise NotImplementedError("This message type is not supported.")

    type: Literal["LLMCallEventMessage"] = "LLMCallEventMessage"


# web request/response data models


class Response(BaseModel):
    message: str
    status: bool
    data: Optional[Any] = None
