"""Native session state for an Actor that owns exactly one conversation."""

import asyncio
import logging
from collections.abc import Mapping
from typing import Any

from google.adk.sessions import DatabaseSessionService
from google.adk.sessions.migration import _schema_check_utils
from google.adk.sessions.schemas.v1 import StorageEvent, StorageMetadata, StorageSession
from sqlalchemy import String, cast, select

from ._request_headers import HEADERS_STATE_KEY, is_credential_header, without_credentials

logger = logging.getLogger(__name__)

# Scoped to one Actor's private database; neither a public Session ID nor a caller.
_CONVERSATION_ID = "conversation"

# Recorded in ADK's metadata table once the stored credential headers are removed.
_CREDENTIAL_HEADERS_REMOVED_KEY = "kagent_credential_headers_removed"


def _has_credential_headers(headers: Any) -> bool:
    return isinstance(headers, Mapping) and any(is_credential_header(name) for name in headers)


def state_without_credential_headers(state: Mapping[str, Any] | None) -> dict[str, Any] | None:
    """The session state with its request headers' credentials removed, or None if it holds none."""
    if not state or not _has_credential_headers(state.get(HEADERS_STATE_KEY)):
        return None
    return {**state, HEADERS_STATE_KEY: without_credentials(state[HEADERS_STATE_KEY])}


def event_data_without_credential_headers(event_data: Mapping[str, Any] | None) -> dict[str, Any] | None:
    """The stored event with its state delta's credential headers removed, or None if it holds none."""
    actions = event_data.get("actions") if event_data else None
    state_delta = actions.get("state_delta") if isinstance(actions, Mapping) else None
    if not isinstance(state_delta, Mapping):
        return None
    cleaned = state_without_credential_headers(state_delta)
    if cleaned is None:
        return None
    return {**event_data, "actions": {**actions, "state_delta": cleaned}}


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

    Request headers are persisted without their credentials (see
    _request_headers.py). A database written before that holds them in the
    session state and in each turn's state delta; prepare_tables removes them
    once, before the first read or write, and records that in ADK's metadata table.
    """

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._credential_headers_removed = False
        self._credential_headers_lock = asyncio.Lock()

    async def prepare_tables(self):
        if self._credential_headers_removed:
            return
        async with self._credential_headers_lock:
            if self._credential_headers_removed:
                return
            await super().prepare_tables()
            await self._remove_stored_credential_headers()
            self._credential_headers_removed = True

    async def _remove_stored_credential_headers(self) -> None:
        # The private store is created by ADK 2.x on its JSON schema; a legacy
        # pickled schema carries no kagent turns to clean.
        if self._db_schema_version != _schema_check_utils.LATEST_SCHEMA_VERSION:
            return
        async with self.database_session_factory() as sql_session:
            if await sql_session.get(StorageMetadata, _CREDENTIAL_HEADERS_REMOVED_KEY) is not None:
                return

            sessions = 0
            for storage_session in (await sql_session.execute(select(StorageSession))).scalars():
                cleaned = state_without_credential_headers(storage_session.state)
                if cleaned is not None:
                    storage_session.state = cleaned
                    sessions += 1

            events = 0
            # Only a turn's first event carries the request headers in its state delta.
            stmt = select(StorageEvent).where(cast(StorageEvent.event_data, String).like(f'%"{HEADERS_STATE_KEY}"%'))
            for storage_event in (await sql_session.execute(stmt)).scalars():
                cleaned = event_data_without_credential_headers(storage_event.event_data)
                if cleaned is not None:
                    storage_event.event_data = cleaned
                    events += 1

            sql_session.add(StorageMetadata(key=_CREDENTIAL_HEADERS_REMOVED_KEY, value="1"))
            await sql_session.commit()
        if not (sessions or events):
            return
        # SQLite keeps rewritten rows' old bytes in free pages until the file is rebuilt.
        if self.db_engine.dialect.name == "sqlite":
            async with self.db_engine.connect() as conn:
                conn = await conn.execution_options(isolation_level="AUTOCOMMIT")
                await conn.exec_driver_sql("VACUUM")
        logger.info(
            "Removed stored credential headers from %d session(s) and %d event(s)",
            sessions,
            events,
        )

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
