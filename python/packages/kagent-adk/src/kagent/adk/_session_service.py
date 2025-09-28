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
            url = f"/api/sessions/{session_id}?user_id={user_id}"
            if config:
                if config.after_timestamp:
                    # TODO: implement
                    # url += f"&after={config.after_timestamp}"
                    pass
                if config.num_recent_events:
                    url += f"&limit={config.num_recent_events}"
                else:
                    url += "&limit=-1"
            else:
                # return all
                url += "&limit=-1"

            # Make API call to get session
            response: httpx.Response = await self.client.get(
                url,
                headers={"X-User-ID": user_id},
            )
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
            return Session(
                id=session_data["id"],
                user_id=session_data["user_id"],
                events=events,
                app_name=app_name,
                state={},  # TODO: restore State
            )
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None
            raise

    @override
    async def list_sessions(self, *, app_name: str, user_id: str) -> ListSessionsResponse:
        # Make API call to list sessions
        response = await self.client.get(f"/api/sessions?user_id={user_id}", headers={"X-User-ID": user_id})
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
            f"/api/sessions/{session_id}?user_id={user_id}",
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

    @override
    async def append_event(self, session: Session, event: Event) -> Event:
        # Convert ADK Event to JSON format
        event_data = {
            "id": event.id,
            "data": event.model_dump_json(),
        }

        # Make API call to append event to session
        response = await self.client.post(
            f"/api/sessions/{session.id}/events?user_id={session.user_id}",
            json=event_data,
            headers={"X-User-ID": session.user_id},
        )
        response.raise_for_status()

        # TODO: potentially pull and update the session from the server
        # Update the in-memory session.
        session.last_update_time = event.timestamp
        await super().append_event(session=session, event=event)

        return event
