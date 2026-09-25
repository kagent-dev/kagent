"""Native session state for an Actor that owns exactly one conversation."""

from google.adk.sessions import DatabaseSessionService

_CONVERSATION_ID = "conversation"


class LocalSessionService(DatabaseSessionService):
    """Keep snapshot state independent of public context IDs and share callers."""

    async def create_session(self, *, app_name, user_id, state=None, session_id=None):
        return await super().create_session(
            app_name=app_name, user_id=_CONVERSATION_ID, session_id=_CONVERSATION_ID, state=state
        )

    async def get_session(self, *, app_name, user_id, session_id, config=None):
        return await super().get_session(
            app_name=app_name, user_id=_CONVERSATION_ID, session_id=_CONVERSATION_ID, config=config
        )

    async def list_sessions(self, *, app_name, user_id=None):
        return await super().list_sessions(app_name=app_name, user_id=_CONVERSATION_ID)

    async def delete_session(self, *, app_name, user_id, session_id):
        await super().delete_session(app_name=app_name, user_id=_CONVERSATION_ID, session_id=_CONVERSATION_ID)
