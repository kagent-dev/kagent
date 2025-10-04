from typing import Any, Optional

from google.adk.events.event import Event
from google.adk.sessions import Session
from google.adk.sessions.base_session_service import (
    BaseSessionService,
    GetSessionConfig,
    ListSessionsResponse,
)
from typing_extensions import override

ACCESS_TOKEN_KEY = "access_token"


class WrappedSessionService(BaseSessionService):
    def __init__(self, wrapped_service, token):
        self._wrapped_service = wrapped_service
        self._token = token

    @override
    async def get_session(
        self, *, app_name: str, user_id: str, session_id: str, config: Optional[GetSessionConfig] = None
    ) -> Optional[Session]:
        session = await self._wrapped_service.get_session(
            app_name=app_name,
            user_id=user_id,
            session_id=session_id,
            config=config,
        )
        if session is not None:
            if session.state is None:
                session.state = {}
            session.state[ACCESS_TOKEN_KEY] = self._token
        return session

    @override
    async def create_session(
        self, *, app_name: str, user_id: str, state: Optional[dict[str, Any]] = None, session_id: Optional[str] = None
    ) -> Session:
        if state is None:
            state = {}
        state[ACCESS_TOKEN_KEY] = self._token
        return await self._wrapped_service.create_session(
            app_name=app_name,
            user_id=user_id,
            state=state,
            session_id=session_id,
        )

    @override
    async def delete_session(self, *, app_name: str, user_id: str, session_id: str) -> None:
        return await self._wrapped_service.delete_session(
            app_name=app_name,
            user_id=user_id,
            session_id=session_id,
        )

    @override
    async def list_sessions(self, *, app_name: str, user_id: str) -> ListSessionsResponse:
        return await self._wrapped_service.list_sessions(
            app_name=app_name,
            user_id=user_id,
        )

    def set_access_token(self, access_token: str) -> None:
        """Update the stored token for future sessions.

        Args:
            access_token: The new access token to store
        """
        self._token = access_token
