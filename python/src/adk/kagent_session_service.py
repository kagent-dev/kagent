import logging
from typing import Any, Optional

import httpx
from google.adk.events.event import Event
from google.adk.sessions import Session
from google.adk.sessions.base_session_service import (
    BaseSessionService,
    GetSessionConfig,
    ListSessionsResponse,
)
from typing_extensions import override

logger = logging.getLogger("kagent." + __name__)


class KagentSessionService(BaseSessionService):
    """A session service implementation that uses the Kagent API.
    This service integrates with the Kagent server to manage session state
    and persistence through HTTP API calls.
    """

    def __init__(self, base_url: str):
        super().__init__()
        self.client = httpx.AsyncClient(base_url=base_url.rstrip("/"))

    async def _get_user_id(self) -> str:
        """Get the default user ID. Override this method to implement custom user ID logic."""
        return "default-user"

    @override
    async def create_session(
        self,
        *,
        app_name: str,
        user_id: str,
        state: Optional[dict[str, Any]] = None,
        session_id: Optional[str] = None,
    ) -> Session:
        # Prepare request data
        request_data = {
            "user_id": user_id,
            "agent_ref": app_name,  # Use app_name as agent reference
        }
        if session_id:
            request_data["name"] = session_id

        # Make API call to create session
        response = await self.client.post(
            "/api/sessions",
            json=request_data,
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        data = response.json()
        if not data.get("data"):
            raise RuntimeError(f"Failed to create session: {data.get('message', 'Unknown error')}")

        session_data = data["data"]

        # Convert to ADK Session format
        return Session(id=session_data["id"], user_id=session_data["user_id"], state=state or {}, app_name=app_name)

    @override
    async def get_session(
        self,
        *,
        app_name: str,
        user_id: str,
        session_id: str,
        config: Optional[GetSessionConfig] = None,
    ) -> Optional[Session]:
        try:
            # Make API call to get session
            response = await self.client.get(
                f"/api/sessions/{session_id}?user_id={user_id}",
                headers={"X-User-ID": user_id},
            )
            response.raise_for_status()

            data = response.json()
            if not data.get("data"):
                return None

            session_data = data["data"]

            # Convert to ADK Session format
            return Session(
                id=session_data["id"],
                user_id=session_data["user_id"],
                app_name="todo",
                state={},  # TODO: restore State
            )
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None
            raise

    @override
    async def list_sessions(self, *, app_name: str, user_id: str) -> ListSessionsResponse:
        # Make API call to list sessions
        response = await self.client.get("/api/sessions", headers={"X-User-ID": user_id})
        response.raise_for_status()

        data = response.json()
        sessions_data = data.get("data", [])

        # Convert to ADK Session format
        sessions = []
        for session_data in sessions_data:
            session = Session(id=session_data["id"], user_id=session_data["user_id"], state={}, app_name=app_name)
            sessions.append(session)

        return ListSessionsResponse(sessions=sessions)

    def list_sessions_sync(self, *, app_name: str, user_id: str) -> ListSessionsResponse:
        raise NotImplementedError("not supported. use async")

    @override
    async def delete_session(self, *, app_name: str, user_id: str, session_id: str) -> None:
        # Make API call to delete session
        response = await self.client.delete(
            f"/api/sessions/{session_id}",
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

    @override
    async def append_event(self, session: Session, event: Event) -> Event:
        # Convert ADK Event to JSON format
        event_data = {
            "type": event.__class__.__name__,
            "data": (event.model_dump() if hasattr(event, "model_dump") else event.__dict__),
            "task_id": event.invocation_id,
        }

        # Make API call to append event to session
        response = await self.client.post(
            f"/api/sessions/{session.id}/events?user_id={session.user_id}",
            json=event_data,
            headers={"X-User-ID": session.user_id},
        )
        response.raise_for_status()

        return event
