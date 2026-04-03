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
        if state and state.get("session_name"):
            request_data["name"] = state.get("session_name", "")
        if state and state.get("source"):
            request_data["source"] = state.get("source", "")

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
            # ADK requires events to be chronological (especially for calculating deltas)
            url = f"/api/sessions/{session_id}?user_id={user_id}&order=asc"
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
            session = Session(
                id=session_data["id"],
                user_id=session_data["user_id"],
                events=events,
                app_name=app_name,
                state={},
            )

            for event in events:
                await super().append_event(session, event)

            return session
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

    async def _recreate_session(self, session: Session) -> None:
        """Recreate a session that was not found (404).

        This handles the case where a session expires or is cleaned up
        during a long-running operation.

        Session State Preservation:
        - session_id: Preserved (same ID used for recreation)
        - user_id: Preserved
        - agent_ref: Preserved
        - session_name: Preserved (from session.state["session_name"])
        - Other session.state fields: NOT preserved (lost on recreation)

        Note: Only session_name is currently used by the application.
        If additional state fields are added in the future, they must be
        explicitly preserved here.

        Args:
            session: The session object to recreate

        Raises:
            httpx.HTTPStatusError: If recreation fails
        """
        request_data = {
            "id": session.id,
            "user_id": session.user_id,
            "agent_ref": session.app_name,
        }
        if session.state and session.state.get("session_name"):
            request_data["name"] = session.state["session_name"]

        # Warn if session has additional state fields that won't be preserved
        if session.state and len(session.state) > 1:
            extra_fields = [k for k in session.state.keys() if k != "session_name"]
            logger.warning(
                "Session %s has additional state fields that will not be preserved during recreation: %s. "
                "Update _recreate_session() if these fields are critical.",
                session.id,
                extra_fields,
            )

        response = await self.client.post(
            "/api/sessions",
            json=request_data,
            headers={"X-User-ID": session.user_id},
        )
        if response.status_code == 409:
            # Session was already recreated by a concurrent call — treat as success.
            logger.info(
                "Session %s already exists (409 Conflict) during recreation, "
                "likely recreated by a concurrent request. Proceeding with retry.",
                session.id,
            )
        else:
            response.raise_for_status()
        logger.info("Successfully recreated session %s", session.id)

        # Fetch existing tasks for this session to check for in-flight work
        tasks_response = await self.client.get(
            f"/api/sessions/{session.id}/tasks?user_id={session.user_id}",
            headers={"X-User-ID": session.user_id},
        )
        if tasks_response.status_code == 200:
            tasks_data = tasks_response.json()
            if tasks_data.get("data"):
                logger.info(
                    "Session %s has %d existing task(s) after recreation",
                    session.id,
                    len(tasks_data["data"]),
                )
                # Log info about in-flight tasks
                for task in tasks_data["data"]:
                    task_status = task.get("status", {})
                    task_state = task_status.get("state", "unknown")
                    if task_state in ("working", "submitted"):
                        logger.info(
                            "Found in-flight task %s in state '%s' - UI should resubscribe to continue receiving updates",
                            task.get("id"),
                            task_state,
                        )
        else:
            logger.warning(
                "Failed to fetch tasks for recreated session %s (HTTP %d). "
                "In-flight task detection unavailable - UI may not auto-reconnect to active tasks.",
                session.id,
                tasks_response.status_code,
            )

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
            headers={"X-User-ID": session.user_id},
        )

        # Handle 404 by recreating session and retrying once
        if response.status_code == 404:
            logger.warning(
                "Session %s not found (404), attempting to recreate before retry",
                session.id,
            )
            await self._recreate_session(session)

            # Retry the append ONCE. If this retry also fails (including another 404),
            # raise_for_status() below will propagate the error without further attempts.
            # This prevents infinite recursion while allowing recovery from transient deletion.
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
