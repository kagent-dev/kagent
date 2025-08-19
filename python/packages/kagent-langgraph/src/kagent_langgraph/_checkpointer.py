"""KAgent Checkpointer for LangGraph.

This module implements a custom checkpointer that persists LangGraph state
to KAgent via REST API calls, storing checkpoints as session events.
"""

import json
import logging
from typing import Any, Dict, AsyncIterator, Iterator, List, Optional, Tuple, override
from uuid import uuid4

import httpx
from langchain_core.runnables import RunnableConfig
from langgraph.checkpoint.base import BaseCheckpointSaver, Checkpoint, CheckpointMetadata, CheckpointTuple

logger = logging.getLogger(__name__)


class KAgentCheckpointer(BaseCheckpointSaver):
    """A checkpointer that stores LangGraph state in KAgent sessions via REST API.

    This checkpointer integrates with the KAgent server to persist graph state
    as session events, enabling distributed execution and session recovery.
    """

    def __init__(self, client: httpx.AsyncClient, app_name: str):
        """Initialize the checkpointer.

        Args:
            client: HTTP client configured with KAgent base URL
            app_name: Application name for session creation
        """
        super().__init__()
        self.client = client
        self.app_name = app_name

    def _extract_config_values(self, config: RunnableConfig) -> Tuple[str, str, str]:
        """Extract required values from config.

        Args:
            config: LangGraph runnable config

        Returns:
            Tuple of (thread_id, user_id, app_name)

        Raises:
            ValueError: If required config values are missing
        """
        configurable = config.get("configurable", {})

        thread_id = configurable.get("thread_id")
        if not thread_id:
            raise ValueError("thread_id is required in config.configurable")

        user_id = configurable.get("user_id", "admin@kagent.dev")
        app_name = configurable.get("app_name", self.app_name)

        return thread_id, user_id, app_name

    async def _ensure_session_exists(self, thread_id: str, user_id: str, app_name: str) -> None:
        """Ensure a session exists in KAgent, creating it if necessary.

        Args:
            thread_id: Session ID (thread_id)
            user_id: User identifier
            app_name: Application name
        """
        try:
            # Check if session exists
            response = await self.client.get(
                f"/api/sessions/{thread_id}?user_id={user_id}", headers={"X-User-ID": user_id}
            )
            if response.status_code == 200:
                return  # Session exists
        except httpx.HTTPStatusError:
            pass  # Session doesn't exist, create it

        # Create session
        request_data = {
            "id": thread_id,
            "user_id": user_id,
            "agent_ref": app_name,
        }

        response = await self.client.post(
            "/api/sessions",
            json=request_data,
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        logger.debug(f"Created session {thread_id} for user {user_id}")

    async def aput(
        self,
        config: RunnableConfig,
        checkpoint: Checkpoint,
        metadata: CheckpointMetadata,
        new_versions: Optional[Dict[str, Any]] = None,
    ) -> RunnableConfig:
        """Store a checkpoint in KAgent as a session event.

        Args:
            config: LangGraph runnable config
            checkpoint: The checkpoint to store
            metadata: Checkpoint metadata
            new_versions: New version information

        Returns:
            Updated config with checkpoint ID
        """
        thread_id, user_id, app_name = self._extract_config_values(config)

        # Ensure session exists
        await self._ensure_session_exists(thread_id, user_id, app_name)

        # Create checkpoint event
        checkpoint_id = checkpoint["id"]
        event_data = {
            "id": str(uuid4()),
            "data": json.dumps(
                {
                    "type": "langgraph_checkpoint",
                    "checkpoint_id": checkpoint_id,
                    "checkpoint": checkpoint,
                    "metadata": metadata,
                    "new_versions": new_versions,
                    "thread_id": thread_id,
                    "user_id": user_id,
                    "app_name": app_name,
                }
            ),
        }

        # Store checkpoint as session event
        response = await self.client.post(
            f"/api/sessions/{thread_id}/events?user_id={user_id}",
            json=event_data,
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        logger.debug(f"Stored checkpoint {checkpoint_id} for session {thread_id}")

        # Return updated config
        new_config = config.copy()
        new_config.setdefault("configurable", {})["checkpoint_id"] = checkpoint_id
        return new_config

    async def aget_tuple(self, config: RunnableConfig) -> Optional[CheckpointTuple]:
        """Retrieve the latest checkpoint for a thread.

        Args:
            config: LangGraph runnable config

        Returns:
            CheckpointTuple if found, None otherwise
        """
        thread_id, user_id, app_name = self._extract_config_values(config)

        try:
            # Get session with all events
            response = await self.client.get(
                f"/api/sessions/{thread_id}?user_id={user_id}&limit=-1", headers={"X-User-ID": user_id}
            )

            if response.status_code == 404:
                return None

            response.raise_for_status()
            data = response.json()

            if not data.get("data") or not data["data"].get("events"):
                return None

            # Find the latest checkpoint event
            latest_checkpoint = None
            for event_data in reversed(data["data"]["events"]):  # Most recent first
                try:
                    event_content = json.loads(event_data["data"])
                    if event_content.get("type") == "langgraph_checkpoint":
                        latest_checkpoint = event_content
                        break
                except (json.JSONDecodeError, KeyError):
                    continue

            if not latest_checkpoint:
                return None

            # Reconstruct checkpoint tuple
            checkpoint = latest_checkpoint["checkpoint"]
            metadata = latest_checkpoint["metadata"]

            return CheckpointTuple(
                config=config,
                checkpoint=checkpoint,
                metadata=metadata,
                parent_config=None,  # TODO: Implement parent tracking if needed
            )

        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None
            raise

    async def alist(
        self,
        config: RunnableConfig | None,
        *,
        filter: Optional[Dict[str, Any]] = None,
        before: Optional[RunnableConfig] = None,
        limit: Optional[int] = None,
    ) -> AsyncIterator[CheckpointTuple]:
        """List checkpoints for a thread.

        Args:
            config: LangGraph runnable config
            filter: Optional filter criteria
            before: Return checkpoints before this config
            limit: Maximum number of checkpoints to return

        Yields:
            CheckpointTuple instances
        """
        thread_id, user_id, app_name = self._extract_config_values(config)

        try:
            # Get session with all events
            response = await self.client.get(
                f"/api/sessions/{thread_id}?user_id={user_id}&limit=-1", headers={"X-User-ID": user_id}
            )

            if response.status_code == 404:
                return

            response.raise_for_status()
            data = response.json()

            if not data.get("data") or not data["data"].get("events"):
                return

            # Find all checkpoint events
            checkpoints = []
            for event_data in data["data"]["events"]:
                try:
                    event_content = json.loads(event_data["data"])
                    if event_content.get("type") == "langgraph_checkpoint":
                        checkpoints.append(event_content)
                except (json.JSONDecodeError, KeyError):
                    continue

            # Sort by checkpoint ID (most recent first)
            checkpoints.sort(key=lambda x: x["checkpoint"]["id"], reverse=True)

            # Apply filters and limits
            count = 0
            for checkpoint_data in checkpoints:
                if limit and count >= limit:
                    break

                # TODO: Implement before filter if needed
                # TODO: Implement additional filters if needed

                checkpoint = checkpoint_data["checkpoint"]
                metadata = checkpoint_data["metadata"]

                checkpoint_config = config.copy()
                checkpoint_config.setdefault("configurable", {})["checkpoint_id"] = checkpoint["id"]

                yield CheckpointTuple(
                    config=checkpoint_config,
                    checkpoint=checkpoint,
                    metadata=metadata,
                    parent_config=None,  # TODO: Implement parent tracking if needed
                )
                count += 1

        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return
            raise

    # Synchronous methods (delegate to async versions)
    def put(
        self,
        config: RunnableConfig,
        checkpoint: Checkpoint,
        metadata: CheckpointMetadata,
        new_versions: Optional[Dict[str, Any]] = None,
    ) -> RunnableConfig:
        """Synchronous version of aput."""
        raise NotImplementedError("Use async version (aput) instead")

    def get_tuple(self, config: RunnableConfig) -> Optional[CheckpointTuple]:
        """Synchronous version of aget_tuple."""
        raise NotImplementedError("Use async version (aget_tuple) instead")

    @override
    def list(
        self,
        config: RunnableConfig,
        *,
        filter: Optional[Dict[str, Any]] = None,
        before: Optional[RunnableConfig] = None,
        limit: Optional[int] = None,
    ) -> Iterator[CheckpointTuple]:
        """Synchronous version of alist."""
        raise NotImplementedError("Use async version (alist) instead")
