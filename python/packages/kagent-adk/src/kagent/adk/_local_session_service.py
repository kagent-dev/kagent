"""Native session state for an Actor that owns exactly one conversation."""

from google.adk.sessions import DatabaseSessionService

# Scoped to one Actor's private database; neither a public Session ID nor a caller.
_CONVERSATION_ID = "conversation"


class LocalSessionService(DatabaseSessionService):
    """Store one Actor's native conversation in its private SQLite database.

    The ADK A2A executor selects state by app name, caller user ID, and context ID.
    A fork gets a new public context but copies this database; a shared Session
    can also have different callers. Fixed user/session keys keep both cases on
    the existing native history. Isolation comes from each Actor's private DB,
    so forks write independently even though they retain the same local keys.
    The configured app name is preserved.

    Shared in-process subagents use this same native conversation. Independent
    child conversations need their own store or distinct keys: this service
    cannot multiplex them. Remote A2A child context/task IDs remain separate.

    Returned sessions expose private keys. Routing, lineage, and external
    correlation must use the executor's public context ID; authorization and
    user memory must use the actual caller. These keys do not grant access.
    append_event can remain inherited because it receives a native session
    returned by create_session/get_session, with the local keys already set.
    """

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
