import logging
from datetime import datetime, timezone
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


class KAgentSessionService(BaseSessionService):
    """A session service implementation that uses the Kagent API.
    This service integrates with the Kagent server to manage session state
    and persistence through HTTP API calls.
    """

    def __init__(self, client: httpx.AsyncClient):
        super().__init__()
        self.client = client

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
            request_data["id"] = session_id
        if state and state.get("session_name"):
            request_data["name"] = state.get("session_name", "")
        if state and state.get("source"):
            request_data["source"] = state.get("source", "")

        # Make API call to create session
        # Pass user_id as a query param so the controller's auth middleware
        # (UnsecureAuthenticator) reads it consistently, matching the user_id
        # used by get_session, list_sessions, delete_session, and append_event.
        # Without this, unsecure-mode requests fall back to "admin@kagent.dev"
        # while all lookups use the A2A-derived user_id, causing SessionNotFoundError.
        response = await self.client.post(
            "/api/sessions",
            params={"user_id": user_id},
            json=request_data,
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
            # ADK requires events to be chronological (especially for calculating deltas).
            # Always fetch the full history: state is built by replaying every event's
            # state_delta below, so limiting the fetch here would silently drop state set
            # by events outside the window. num_recent_events is applied after, by
            # trimming session.events once state is already correct.
            params: dict[str, str | int] = {"user_id": user_id, "order": "asc", "limit": -1}
            if config and config.after_timestamp is not None:
                params["after"] = datetime.fromtimestamp(config.after_timestamp, tz=timezone.utc).isoformat()

            # Make API call to get session
            response: httpx.Response = await self.client.get(f"/api/sessions/{session_id}", params=params)
            if response.status_code == 404:
                return None
            response.raise_for_status()

            data = response.json()
            if not data.get("data"):
                return None

            if not data.get("data").get("session"):
                return None
            session_data = data["data"]["session"]

            events_data = data["data"]["events"]

            events: list[Event] = []
            for event_data in events_data:
                events.append(Event.model_validate_json(event_data["data"]))

            # Convert to ADK Session format
            session = Session(
                id=session_data["id"],
                user_id=session_data["user_id"],
                events=[],
                app_name=app_name,
                state={},
            )

            for event in events:
                await super().append_event(session, event)

            if config and config.num_recent_events is not None:
                # Trim only after every event has been replayed, so state is complete.
                # num_recent_events == 0 means "no events", not "all events" ([-0:] would
                # keep everything).
                num_recent_events = config.num_recent_events
                session.events = session.events[-num_recent_events:] if num_recent_events else []

            return session
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None
            raise

    @override
    async def list_sessions(self, *, app_name: str, user_id: str) -> ListSessionsResponse:
        # Make API call to list sessions
        response = await self.client.get(f"/api/sessions?user_id={user_id}")
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
        response = await self.client.delete(f"/api/sessions/{session_id}?user_id={user_id}")
        response.raise_for_status()

    @override
    async def append_event(self, session: Session, event: Event) -> Event:
        if event.partial:
            return event

        # Convert ADK Event to JSON format
        event_data = {
            "id": event.id,
            "data": event.model_dump_json(),
        }

        # Make API call to append event to session
        response = await self.client.post(
            f"/api/sessions/{session.id}/events?user_id={session.user_id}",
            json=event_data,
        )
        response.raise_for_status()

        # TODO: potentially pull and update the session from the server
        # Update the in-memory session.
        session.last_update_time = event.timestamp
        await super().append_event(session=session, event=event)

        return event
