import asyncio
import logging
import traceback
from datetime import datetime, timezone
from typing import Any, AsyncGenerator, Optional, Sequence, Union

from autogen_agentchat.base import TaskResult
from autogen_agentchat.messages import (
    BaseAgentEvent,
    BaseChatMessage,
    ChatMessage,
    HandoffMessage,
    MemoryQueryEvent,
    MessageFactory,
    ModelClientStreamingChunkEvent,
    MultiModalMessage,
    StopMessage,
    TextMessage,
    ToolCallExecutionEvent,
    ToolCallRequestEvent,
    ToolCallSummaryMessage,
)
from autogen_core import CancellationToken, ComponentModel
from autogen_core import Image as AGImage
from fastapi import WebSocket, WebSocketDisconnect

from ..database import DatabaseManager
from ..datamodel import (
    LLMCallEventMessage,
    Message,
    MessageConfig,
    Run,
    RunStatus,
    Session,
    SettingsConfig,
    Team,
    TeamResult,
)
from ..teammanager import TeamManager
from ..web.managers.run_context import RunContext
from ..web.routes.invoke import format_message, format_team_result

logger = logging.getLogger(__name__)


class SessionManager:
    """Manages WebSocket connections and message streaming for team task execution"""

    def __init__(self, db_manager: DatabaseManager):
        self.db_manager = db_manager
        self.message_factory = MessageFactory()

        self._cancel_message = TeamResult(
            task_result=TaskResult(
                messages=[TextMessage(source="user", content="Run cancelled by user")], stop_reason="cancelled by user"
            ),
            usage="",
            duration=0,
        ).model_dump()

    def _get_stop_message(self, reason: str) -> dict:
        return TeamResult(
            task_result=TaskResult(messages=[TextMessage(source="user", content=reason)], stop_reason=reason),
            usage="",
            duration=0,
        ).model_dump()

    def _convert_message_config_to_chat_message(self, raw_messages: list[dict]) -> list[BaseChatMessage]:
        """Convert MessageConfig to appropriate BaseChatMessage type using MessageFactory"""

        messages = []
        for message_config in raw_messages:
            message = self.message_factory.create(message_config)
            if isinstance(message, BaseChatMessage):
                messages.append(message)

        return messages

    async def _get_session_messages(self, session_id: int) -> Sequence[BaseChatMessage]:
        """Get all previous messages for a session and convert them to BaseChatMessage format"""
        messages_response = self.db_manager.get(
            Message, filters={"session_id": session_id}, order="asc", return_json=False
        )

        if not messages_response.status or not messages_response.data:
            return []

        chat_messages: list[dict] = []
        for message in messages_response.data:
            chat_messages.append(message.config)

        return self._convert_message_config_to_chat_message(chat_messages)

    def _prepare_task_with_history(
        self,
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

    async def start(
        self,
        user_id: str,
        run_id: int,
        task: str,
        team_config: Union[ComponentModel, dict],
    ) -> TeamResult:
        """Start a run"""

        with RunContext.populate_context(run_id=run_id):
            team_manager = TeamManager()

            try:
                # Get run and session info
                run = await self._get_run(run_id)
                if run is None:
                    raise ValueError(f"Run {run_id} not found")

                if run.session_id is None:
                    raise ValueError(f"Run {run_id} has no session_id")

                session = await self._get_session(run.session_id)
                if session is None:
                    raise ValueError(f"Session {run.session_id} not found")

                # Get previous messages for the session
                previous_messages = await self._get_session_messages(run.session_id)

                await self._update_run(run_id, RunStatus.ACTIVE)

                # Prepare task with message history
                prepared_task = self._prepare_task_with_history(task, previous_messages)
                result = await team_manager.run(prepared_task, team_config)

                # Remove n messages from result, where n is len(previous_messages)
                result.task_result.messages = result.task_result.messages[len(previous_messages) :]
                for message in result.task_result.messages:
                    await self._save_message(user_id, run_id, message)
                await self._update_run(
                    run_id, RunStatus.COMPLETE, team_result=result.model_dump(exclude={"created_at"})
                )
                return result
            except Exception as e:
                await self._update_run(run_id, RunStatus.ERROR, error=str(e))
                raise e

    async def start_stream(
        self,
        user_id: str,
        run_id: int,
        task: str,
        team_config: Union[ComponentModel, dict],
    ) -> AsyncGenerator[dict, None]:
        """Start streaming task execution with proper run management"""

        with RunContext.populate_context(run_id=run_id):
            team_manager = TeamManager()
            cancellation_token = CancellationToken()
            final_result = None

            try:
                # Get run and session info
                run = await self._get_run(run_id)
                if run is None:
                    raise ValueError(f"Run {run_id} not found")

                if run.session_id is None:
                    raise ValueError(f"Run {run_id} has no session_id")

                session = await self._get_session(run.session_id)
                if session is None:
                    raise ValueError(f"Session {run.session_id} not found")

                # Get previous messages for the session
                previous_messages = await self._get_session_messages(run.session_id)

                await self._update_run(run_id, RunStatus.ACTIVE)

                # Prepare task with message history
                prepared_task: str | BaseChatMessage | Sequence[BaseChatMessage] | None = (
                    self._prepare_task_with_history(task, previous_messages)
                )
                # ignore first  n messages from result, where n is len(previous_messages)
                num_previous_messages = len(previous_messages)
                async for message in team_manager.run_stream(
                    task=prepared_task,
                    team_config=team_config,
                    cancellation_token=cancellation_token,
                ):
                    if num_previous_messages > 0:
                        num_previous_messages -= 1
                        continue
                    if isinstance(message, TeamResult):
                        message.task_result.messages = message.task_result.messages[num_previous_messages:]
                        formatted_message = format_team_result(message)
                        yield formatted_message
                        final_result = formatted_message
                    elif isinstance(
                        message,
                        (
                            TextMessage,
                            MultiModalMessage,
                            StopMessage,
                            HandoffMessage,
                            ToolCallRequestEvent,
                            ToolCallExecutionEvent,
                            ToolCallSummaryMessage,
                            LLMCallEventMessage,
                            MemoryQueryEvent,
                        ),
                    ):
                        message_id = await self._save_message(user_id, run_id, message)
                        if message_id:
                            message.metadata["id"] = str(message_id)
                        formatted_message = format_message(message)
                        yield formatted_message
                    elif isinstance(message, ModelClientStreamingChunkEvent):
                        formatted_message = format_message(message)
                        yield formatted_message

                if final_result:
                    await self._update_run(run_id, RunStatus.COMPLETE, team_result=final_result)
                else:
                    logger.warning(f"No final result captured for completed run {run_id}")
                    await self._update_run_status(run_id, RunStatus.COMPLETE)

            except Exception as e:
                logger.error(f"Stream error for run {run_id}: {e}")
                traceback.print_exc()

                # The messages[0].content isn't properly being serialized, so it
                # doesn't even get sent back to the client. (That's why we're seeing undefined in the UI)
                # I am using the stop_reason to send the error message back and specifically checking
                # for the message type (error).
                error_result = TeamResult(
                    task_result=TaskResult(
                        messages=[TextMessage(source="system", content=str(e))],
                        stop_reason=str(e),
                    ),
                    usage="",
                    duration=0,
                ).model_dump()
                await self._update_run(run_id, RunStatus.ERROR, team_result=error_result, error=str(e))
                yield {"type": "error", "data": error_result}

    async def _save_message(
        self, user_id: str, run_id: int, message: Union[BaseAgentEvent | BaseChatMessage, BaseChatMessage]
    ) -> Optional[int]:
        """Save a message to the database"""

        run = await self._get_run(run_id)
        if run and run.session_id is not None:
            db_message = Message(
                session_id=run.session_id,
                run_id=run_id,
                config=self._convert_images_in_dict(message.model_dump(exclude={"created_at"})),
                user_id=user_id,
            )
            response = self.db_manager.upsert(db_message, return_json=False)
            if response.status and response.data:
                return response.data.id
            return None

    async def _update_run(
        self, run_id: int, status: RunStatus, team_result: Optional[dict] = None, error: Optional[str] = None
    ) -> None:
        """Update run status and result"""
        run = await self._get_run(run_id)
        if run:
            run.status = status
            if team_result:
                run.team_result = self._convert_images_in_dict(team_result)
            if error:
                run.error_message = error
            self.db_manager.upsert(run)

    def _convert_images_in_dict(self, obj: Any) -> Any:
        """Recursively find and convert Image objects in dictionaries and lists"""
        if isinstance(obj, dict):
            return {k: self._convert_images_in_dict(v) for k, v in obj.items()}
        elif isinstance(obj, list):
            return [self._convert_images_in_dict(item) for item in obj]
        elif isinstance(obj, AGImage):  # Assuming you've imported AGImage
            # Convert the Image object to a serializable format
            return {"type": "image", "url": f"data:image/png;base64,{obj.to_base64()}", "alt": "Image"}
        else:
            return obj

    async def _get_run(self, run_id: int) -> Optional[Run]:
        """Get run from database

        Args:
            run_id: id of the run to retrieve

        Returns:
            Optional[Run]: Run object if found, None otherwise
        """
        response = self.db_manager.get(Run, filters={"id": run_id}, return_json=False)
        return response.data[0] if response.status and response.data else None

    async def _get_session(self, session_id: int) -> Optional[Session]:
        """Get session from database

        Args:
            session_id: id of the session to retrieve

        Returns:
            Optional[Session]: Session object if found, None otherwise
        """
        response = self.db_manager.get(Session, filters={"id": session_id}, return_json=False)
        return response.data[0] if response.status and response.data else None

    async def _update_run_status(self, run_id: int, status: RunStatus, error: Optional[str] = None) -> None:
        """Update run status in database

        Args:
            run_id: id of the run to update
            status: New status to set
            error: Optional error message
        """
        run = await self._get_run(run_id)
        if run:
            run.status = status
            run.error_message = error
            self.db_manager.upsert(run)
