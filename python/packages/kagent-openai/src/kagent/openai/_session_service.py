"""KAgent Session Service for OpenAI Agents SDK.

This module implements the OpenAI Agents SDK SessionABC protocol,
storing session data through the KAgent controller SessionService.
"""

from __future__ import annotations

import json
import logging
import uuid
from datetime import UTC, datetime

import grpc
from agents.items import TResponseInputItem
from agents.memory.session import SessionABC
from kagent.api.v1alpha1 import sessions_pb2
from kagent.core import AsyncControllerClient

logger = logging.getLogger(__name__)


class KAgentSession(SessionABC):
    """OpenAI Agents SDK session backed by generated Session RPCs."""

    def __init__(
        self,
        session_id: str,
        client: AsyncControllerClient,
        app_name: str,
        user_id: str,
    ):
        """Initialize a KAgent session.

        Args:
            session_id: Unique identifier for this session
            client: Shared authenticated controller gRPC client
            app_name: Application name for session tracking
            user_id: User identifier for session scoping
        """
        self.session_id = session_id
        self.client = client
        self.app_name = app_name
        self.user_id = user_id
        self._items_cache: list[TResponseInputItem] | None = None

    async def _ensure_session_exists(self) -> None:
        """Ensure the session exists in KAgent backend, creating if needed."""
        try:
            await self.client.session_service.GetSession(
                sessions_pb2.GetSessionRequest(
                    session_id=self.session_id,
                    order=sessions_pb2.EVENT_ORDER_DESCENDING,
                    limit=1,
                ),
                **await self.client.call_options(self.user_id),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.NOT_FOUND:
                await self._create_session()
                return
            raise

    async def _create_session(self) -> None:
        """Create a new session in KAgent backend."""
        response = await self.client.session_service.CreateSession(
            sessions_pb2.CreateSessionRequest(id=self.session_id, agent_ref=self.app_name),
            **await self.client.call_options(self.user_id),
        )
        if not response.HasField("session"):
            raise RuntimeError("failed to create session: response did not include a session")

        logger.debug("Created session %s for user %s", self.session_id, self.user_id)

    async def get_items(self, limit: int | None = None) -> list[TResponseInputItem]:
        """Retrieve conversation history for this session.

        Args:
            limit: Maximum number of items to retrieve (None for all)

        Returns:
            List of conversation items from the session
        """
        try:
            response = await self.client.session_service.GetSession(
                sessions_pb2.GetSessionRequest(
                    session_id=self.session_id,
                    order=sessions_pb2.EVENT_ORDER_ASCENDING,
                ),
                **await self.client.call_options(self.user_id),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.NOT_FOUND:
                return []
            raise

        items: list[TResponseInputItem] = []
        for event in response.events:
            try:
                event_obj = json.loads(event.data)
            except (json.JSONDecodeError, TypeError) as error:
                logger.warning("Failed to parse event data: %s", error)
                continue
            if isinstance(event_obj, dict) and isinstance(event_obj.get("items"), list):
                items.extend(event_obj["items"])

        if limit is not None and limit > 0:
            items = items[-limit:]

        self._items_cache = items
        return items

    async def add_items(self, items: list[TResponseInputItem]) -> None:
        """Store new items for this session.

        Args:
            items: List of conversation items to add to the session
        """
        if not items:
            return

        # Ensure session exists before adding items
        await self._ensure_session_exists()

        await self.client.session_service.AddSessionEvent(
            sessions_pb2.AddSessionEventRequest(
                session_id=self.session_id,
                id=str(uuid.uuid4()),
                data=json.dumps(
                    {
                        "timestamp": datetime.now(UTC).isoformat(),
                        "items": items,
                        "type": "conversation_items",
                    }
                ),
            ),
            **await self.client.call_options(self.user_id),
        )

        # Update cache
        if self._items_cache is not None:
            self._items_cache.extend(items)

        logger.debug("Added %d items to session %s", len(items), self.session_id)

    async def pop_item(self) -> TResponseInputItem | None:
        """Remove and return the most recent item from this session.

        Returns:
            The most recent item, or None if session is empty
        """
        # Get all items
        items = await self.get_items()

        if not items:
            return None

        # Pop the last item
        last_item = items.pop()

        # Clear the session and re-add remaining items
        # This is inefficient but matches the expected behavior
        # A production implementation might use a more efficient approach
        await self.clear_session()
        if items:
            await self.add_items(items)

        # Update cache
        self._items_cache = items

        return last_item

    async def clear_session(self) -> None:
        """Clear all items for this session."""
        try:
            await self.client.session_service.DeleteSession(
                sessions_pb2.DeleteSessionRequest(session_id=self.session_id),
                **await self.client.call_options(self.user_id),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() != grpc.StatusCode.NOT_FOUND:
                raise
        self._items_cache = None
        logger.debug("Cleared session %s", self.session_id)


class KAgentSessionFactory:
    """Factory for sessions sharing one controller gRPC client."""

    def __init__(
        self,
        client: AsyncControllerClient,
        app_name: str,
        default_user_id: str = "admin@kagent.dev",
    ):
        """Initialize the session factory.

        Args:
            client: Shared authenticated controller gRPC client
            app_name: Application name for session tracking
            default_user_id: Default user ID if not specified per session
        """
        self.client = client
        self.app_name = app_name
        self.default_user_id = default_user_id

    def create_session(
        self,
        session_id: str,
        user_id: str | None = None,
    ) -> KAgentSession:
        """Create a new session instance.

        Args:
            session_id: Unique identifier for the session
            user_id: Optional user ID (uses default if not provided)

        Returns:
            A new KAgentSession instance
        """
        return KAgentSession(
            session_id=session_id,
            client=self.client,
            app_name=self.app_name,
            user_id=user_id or self.default_user_id,
        )
