"""KAgent Session Service for OpenAI Agents SDK.

This module implements the OpenAI Agents SDK SessionABC protocol,
storing session data in the KAgent backend via REST API.
"""

from __future__ import annotations

import logging

import httpx
from agents.items import TResponseInputItem
from agents.memory.session import SessionABC

logger = logging.getLogger(__name__)


class KAgentSession(SessionABC):
    """A session implementation that uses the KAgent API.

    This session integrates with the KAgent server to manage session state
    and persistence through HTTP API calls, implementing the OpenAI Agents SDK
    SessionABC protocol.
    """

    def __init__(
        self,
        session_id: str,
        client: httpx.AsyncClient,
        app_name: str,
        user_id: str,
    ):
        """Initialize a KAgent session.

        Args:
            session_id: Unique identifier for this session
            client: HTTP client for making API calls
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
            # Try to get the session
            response = await self.client.get(
                f"/api/sessions/{self.session_id}?user_id={self.user_id}&limit=0",
                headers={"X-User-ID": self.user_id, "X-Agent-Name": self.app_name},
            )
            if response.status_code == 404:
                # Session doesn't exist, create it
                await self._create_session()
            else:
                response.raise_for_status()
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                await self._create_session()
            else:
                raise

    async def _create_session(self) -> None:
        """Create a new session in KAgent backend."""
        request_data = {
            "id": self.session_id,
            "user_id": self.user_id,
            "agent_ref": self.app_name,
        }

        response = await self.client.post(
            "/api/sessions",
            json=request_data,
            headers={"X-User-ID": self.user_id, "X-Agent-Name": self.app_name},
        )
        response.raise_for_status()

        data = response.json()
        if not data.get("data"):
            raise RuntimeError(f"Failed to create session: {data.get('message', 'Unknown error')}")

        logger.debug(f"Created session {self.session_id} for user {self.user_id}")

    async def get_items(self, limit: int | None = None) -> list[TResponseInputItem]:
        """Retrieve conversation history for this session.

        Args:
            limit: Maximum number of items to retrieve (None for all)

        Returns:
            List of conversation items from the session
        """
        try:
            # Build URL with limit parameter
            url = f"/api/sessions/{self.session_id}?user_id={self.user_id}"
            if limit is not None:
                url += f"&limit={limit}"
            else:
                url += "&limit=-1"  # -1 means all items

            response = await self.client.get(
                url,
                headers={"X-User-ID": self.user_id, "X-Agent-Name": self.app_name},
            )

            if response.status_code == 404:
                # Session doesn't exist yet, return empty list
                return []

            response.raise_for_status()
            data = response.json()

            if not data.get("data") or not data["data"].get("events"):
                return []

            # Convert stored events back to OpenAI items format
            items: list[TResponseInputItem] = []
            events_data = data["data"]["events"]

            for event_data in events_data:
                # Events are stored as JSON strings in the 'data' field
                event_json = event_data.get("data")
                if event_json:
                    # Parse the event and extract items if they exist
                    import json

                    try:
                        event_obj = json.loads(event_json)
                        # Look for items in the event
                        if "items" in event_obj:
                            items.extend(event_obj["items"])
                    except (json.JSONDecodeError, TypeError) as e:
                        logger.warning(f"Failed to parse event data: {e}")
                        continue

            # Apply limit if specified
            if limit is not None and limit > 0:
                items = items[-limit:]

            self._items_cache = items
            return items

        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return []
            raise

    async def add_items(self, items: list[TResponseInputItem]) -> None:
        """Store new items for this session.

        Args:
            items: List of conversation items to add to the session
        """
        if not items:
            return

        # Ensure session exists before adding items
        await self._ensure_session_exists()

        # Store items as an event in the session
        import json
        import uuid
        from datetime import UTC, datetime

        event_data = {
            "id": str(uuid.uuid4()),
            "data": json.dumps(
                {
                    "timestamp": datetime.now(UTC).isoformat(),
                    "items": items,
                    "type": "conversation_items",
                }
            ),
        }

        response = await self.client.post(
            f"/api/sessions/{self.session_id}/events?user_id={self.user_id}",
            json=event_data,
            headers={"X-User-ID": self.user_id, "X-Agent-Name": self.app_name},
        )
        response.raise_for_status()

        # Update cache
        if self._items_cache is not None:
            self._items_cache.extend(items)

        logger.debug(f"Added {len(items)} items to session {self.session_id}")

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
            # Delete the session from KAgent backend
            response = await self.client.delete(
                f"/api/sessions/{self.session_id}?user_id={self.user_id}",
                headers={"X-User-ID": self.user_id, "X-Agent-Name": self.app_name},
            )
            response.raise_for_status()

            # Clear cache
            self._items_cache = None

            logger.debug(f"Cleared session {self.session_id}")

        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                # Session doesn't exist, that's fine
                self._items_cache = None
            else:
                raise


class KAgentSessionFactory:
    """Factory for creating KAgent sessions.

    This factory manages the HTTP client and configuration needed to create
    KAgentSession instances that communicate with the KAgent backend.
    """

    def __init__(
        self,
        client: httpx.AsyncClient,
        app_name: str,
        default_user_id: str = "admin@kagent.dev",
    ):
        """Initialize the session factory.

        Args:
            client: HTTP client for making API calls to KAgent
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
