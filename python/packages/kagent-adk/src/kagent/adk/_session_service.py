import logging
from datetime import datetime, timezone
from typing import Any, Optional

import grpc
from google.adk.events.event import Event
from google.adk.sessions import Session
from google.adk.sessions.base_session_service import (
    BaseSessionService,
    GetSessionConfig,
    ListSessionsResponse,
)
from google.protobuf.timestamp_pb2 import Timestamp
from kagent.api.v1alpha1 import sessions_pb2
from kagent.core import AsyncControllerClient
from typing_extensions import override

logger = logging.getLogger("kagent." + __name__)


class KAgentSessionService(BaseSessionService):
    """ADK session persistence backed by the controller SessionService."""

    def __init__(self, client: AsyncControllerClient):
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
        request = sessions_pb2.CreateSessionRequest(agent_ref=app_name)
        if session_id:
            request.id = session_id
        if state and state.get("session_name"):
            request.name = state["session_name"]
        if state and state.get("source"):
            request.source = _session_source(state["source"])

        response = await self.client.session_service.CreateSession(
            request,
            **await self.client.call_options(user_id),
        )
        if not response.HasField("session"):
            raise RuntimeError("failed to create session: response did not include a session")
        return Session(
            id=response.session.id,
            user_id=response.session.user_id,
            state=state or {},
            app_name=app_name,
        )

    @override
    async def get_session(
        self,
        *,
        app_name: str,
        user_id: str,
        session_id: str,
        config: Optional[GetSessionConfig] = None,
    ) -> Optional[Session]:
        request = sessions_pb2.GetSessionRequest(
            session_id=session_id,
            order=sessions_pb2.EVENT_ORDER_ASCENDING,
        )
        if config and config.after_timestamp is not None:
            after = Timestamp()
            after.FromDatetime(datetime.fromtimestamp(config.after_timestamp, tz=timezone.utc))
            request.after.CopyFrom(after)

        try:
            response = await self.client.session_service.GetSession(
                request,
                **await self.client.call_options(user_id),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.NOT_FOUND:
                return None
            raise
        if not response.HasField("session"):
            return None

        session = Session(
            id=response.session.id,
            user_id=response.session.user_id,
            events=[],
            app_name=app_name,
            state={},
        )
        for event_data in response.events:
            await super().append_event(session, Event.model_validate_json(event_data.data))

        if config and config.num_recent_events is not None:
            num_recent_events = config.num_recent_events
            session.events = session.events[-num_recent_events:] if num_recent_events else []

        return session

    @override
    async def list_sessions(self, *, app_name: str, user_id: str) -> ListSessionsResponse:
        response = await self.client.session_service.ListSessions(
            sessions_pb2.ListSessionsRequest(),
            **await self.client.call_options(user_id),
        )
        sessions = [
            Session(id=value.id, user_id=value.user_id, state={}, app_name=app_name) for value in response.sessions
        ]
        return ListSessionsResponse(sessions=sessions)

    def list_sessions_sync(self, *, app_name: str, user_id: str) -> ListSessionsResponse:
        raise NotImplementedError("not supported. use async")

    @override
    async def delete_session(self, *, app_name: str, user_id: str, session_id: str) -> None:
        await self.client.session_service.DeleteSession(
            sessions_pb2.DeleteSessionRequest(session_id=session_id),
            **await self.client.call_options(user_id),
        )

    @override
    async def append_event(self, session: Session, event: Event) -> Event:
        if event.partial:
            return event

        await self.client.session_service.AddSessionEvent(
            sessions_pb2.AddSessionEventRequest(
                session_id=session.id,
                id=event.id,
                data=event.model_dump_json(),
            ),
            **await self.client.call_options(session.user_id),
        )
        session.last_update_time = event.timestamp
        await super().append_event(session=session, event=event)

        return event


def _session_source(value: str) -> int:
    match value.lower():
        case "user":
            return sessions_pb2.SESSION_SOURCE_USER
        case "agent":
            return sessions_pb2.SESSION_SOURCE_AGENT
        case _:
            raise ValueError(f"unsupported session source {value!r}")
