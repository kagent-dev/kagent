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
    ToolCallSummaryMessage,
    ModelClientStreamingChunkEvent,
    MultiModalMessage,
    StopMessage,
    TextMessage,
    ToolCallExecutionEvent,
    ToolCallRequestEvent,
)
from autogen_core import CancellationToken
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

# from .run_context import RunContext

logger = logging.getLogger(__name__)


class SessionManager:
    """Manages WebSocket connections and message streaming for team task execution"""

    def __init__(self, db_manager: DatabaseManager):
        self.db_manager = db_manager

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

    async def start(
        self, user_id: str, run_id: int, task: str | ChatMessage | Sequence[ChatMessage] | None
    ) -> TeamResult:
        """Start a run"""

        with RunContext.populate_context(run_id=run_id):
            team_manager = TeamManager()
            # cancellation_token = CancellationToken()
            # final_result = None

            try:
                # Update run with task and status
                run = await self._get_run(run_id)
                if run is None:
                    raise ValueError(f"Run {run_id} not found")
                session = await self._get_session(run.session_id)
                if session is None:
                    raise ValueError(f"Session {run.session_id} not found")
                team = await self._get_team(session.team_id)
                if team is None:
                    raise ValueError(f"Team {session.team_id} not found")

                await self._update_run(run_id, RunStatus.ACTIVE)
                result = await team_manager.run(task, team.component, state=session.team_state)
                if team_manager._team:
                    state = await team_manager._team.save_state()
                    await self._update_session_state(session.id, state)
                for message in result.task_result.messages:
                    await self._save_message(user_id, run_id, message)
                await self._update_run(run_id, RunStatus.COMPLETE, team_result=result.model_dump())
                return result
            except Exception as e:
                await self._update_run(run_id, RunStatus.ERROR, error=str(e))
                raise e

    async def start_stream(
        self, user_id: str, run_id: int, task: str | ChatMessage | Sequence[ChatMessage] | None
    ) -> AsyncGenerator[dict, None]:
        """Start streaming task execution with proper run management"""

        with RunContext.populate_context(run_id=run_id):
            team_manager = TeamManager()
            cancellation_token = CancellationToken()
            final_result = None
            session: Optional[Session] = None
            try:
                # Update run with task and status
                run = await self._get_run(run_id)
                if run is None:
                    raise ValueError(f"Run {run_id} not found")
                session = await self._get_session(run.session_id)
                if session is None:
                    raise ValueError(f"Session {run.session_id} not found")
                team = await self._get_team(session.team_id)
                if team is None:
                    raise ValueError(f"Team {session.team_id} not found")

                await self._update_run(run_id, RunStatus.ACTIVE)

                async for message in team_manager.run_stream(
                    task=task,
                    team_config=team.component,
                    cancellation_token=cancellation_token,
                    state=session.team_state,
                ):
                    if isinstance(message, TeamResult):
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
                        formatted_message = format_message(message)
                        yield formatted_message
                        await self._save_message(user_id, run_id, message)
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

                error_result = TeamResult(
                    task_result=TaskResult(
                        messages=[TextMessage(source="system", content=str(e))],
                        stop_reason="An error occurred while processing this run",
                    ),
                    usage="",
                    duration=0,
                ).model_dump()
                await self._update_run(run_id, RunStatus.ERROR, team_result=error_result, error=str(e))
                yield {"type": "error", "data": error_result}
                # Save team state to session
                if team_manager._team and session:
                    state = await team_manager._team.save_state()
                    await self._update_session_state(session.id, state)
            finally:
                # Save team state to session
                if team_manager._team and session:
                    state = await team_manager._team.save_state()
                    await self._update_session_state(session.id, state)

    async def _save_message(
        self, user_id: str, run_id: int, message: Union[BaseAgentEvent | BaseChatMessage, BaseChatMessage]
    ) -> None:
        """Save a message to the database"""

        run = await self._get_run(run_id)
        if run:
            db_message = Message(
                session_id=run.session_id,
                run_id=run_id,
                config=self._convert_images_in_dict(message.model_dump()),
                user_id=user_id,
            )
            self.db_manager.upsert(db_message)

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

    async def _update_session_state(self, session_id: int, team_state: dict) -> None:
        """Update session state"""
        session = await self._get_session(session_id)
        if session:
            session.team_state = team_state
            self.db_manager.upsert(session)

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

    async def _get_team(self, team_id: int) -> Optional[Team]:
        """Get team from database

        Args:
            team_id: id of the team to retrieve

        Returns:
            Optional[Team]: Team object if found, None otherwise
        """
        response = self.db_manager.get(Team, filters={"id": team_id}, return_json=False)
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
