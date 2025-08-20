"""KAgent Remote Checkpointer for LangGraph.

This module implements a remote checkpointer that calls the KAgent Go service
for LangGraph checkpoint persistence via HTTP API.
"""

import logging
from collections.abc import AsyncIterator, Iterator
from typing import Any, override

import httpx
from langchain_core.runnables import RunnableConfig
from langgraph.checkpoint.base import BaseCheckpointSaver, Checkpoint, CheckpointMetadata, CheckpointTuple

logger = logging.getLogger(__name__)


class KAgentCheckpointer(BaseCheckpointSaver):
    """A remote checkpointer that stores LangGraph state in KAgent via the Go service.

    This checkpointer calls the KAgent Go HTTP service to persist graph state,
    enabling distributed execution and session recovery.
    """

    def __init__(self, client: httpx.AsyncClient, app_name: str):
        """Initialize the checkpointer.

        Args:
            client: HTTP client configured with KAgent base URL
            app_name: Application name (used for checkpoint namespace if not specified)
        """
        super().__init__()
        self.client = client
        self.app_name = app_name

    def _extract_config_values(self, config: RunnableConfig) -> tuple[str, str, str]:
        """Extract required values from config.

        Args:
            config: LangGraph runnable config

        Returns:
            Tuple of (thread_id, user_id, checkpoint_ns)

        Raises:
            ValueError: If required config values are missing
        """
        configurable = config.get("configurable", {})

        thread_id = configurable.get("thread_id")
        if not thread_id:
            raise ValueError("thread_id is required in config.configurable")

        user_id = configurable.get("user_id", "admin@kagent.dev")
        checkpoint_ns = configurable.get("checkpoint_ns", "")

        return thread_id, user_id, checkpoint_ns

    @override
    async def aput(
        self,
        config: RunnableConfig,
        checkpoint: Checkpoint,
        metadata: CheckpointMetadata,
        new_versions: dict[str, Any] | None = None,
    ) -> RunnableConfig:
        """Store a checkpoint via the KAgent Go service.

        Args:
            config: LangGraph runnable config
            checkpoint: The checkpoint to store
            metadata: Checkpoint metadata
            new_versions: New version information (stored in metadata)

        Returns:
            Updated config with checkpoint ID
        """
        thread_id, user_id, checkpoint_ns = self._extract_config_values(config)

        # Prepare request data
        request_data = {
            "thread_id": thread_id,
            "checkpoint_ns": checkpoint_ns,
            "checkpoint_id": checkpoint["id"],
            "checkpoint": checkpoint,
            "metadata": metadata,
            "version": 1,
        }

        # Add parent checkpoint ID if available
        if "parent_config" in checkpoint:
            parent_config = checkpoint["parent_config"]
            if isinstance(parent_config, dict) and "configurable" in parent_config:
                parent_checkpoint_id = parent_config["configurable"].get("checkpoint_id")
                if parent_checkpoint_id:
                    request_data["parent_checkpoint_id"] = parent_checkpoint_id

        # Store new_versions in metadata if provided
        if new_versions:
            metadata = dict(metadata) if metadata else {}
            metadata["new_versions"] = new_versions
            request_data["metadata"] = metadata

        # Call the Go service
        response = await self.client.post(
            "/api/langgraph/checkpoints",
            json=request_data,
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        logger.debug(f"Stored checkpoint {checkpoint['id']} for thread {thread_id}")

        # Return updated config
        new_config = config.copy()
        new_config.setdefault("configurable", {})["checkpoint_id"] = checkpoint["id"]
        return new_config

    @override
    async def aget_tuple(self, config: RunnableConfig) -> CheckpointTuple | None:
        """Retrieve the latest checkpoint for a thread.

        Args:
            config: LangGraph runnable config

        Returns:
            CheckpointTuple if found, None otherwise
        """
        thread_id, user_id, checkpoint_ns = self._extract_config_values(config)

        try:
            # Call the Go service for latest checkpoint
            params = {
                "thread_id": thread_id,
                "checkpoint_ns": checkpoint_ns,
            }

            response = await self.client.get(
                "/api/langgraph/checkpoints/latest", params=params, headers={"X-User-ID": user_id}
            )

            if response.status_code == 404:
                return None

            response.raise_for_status()
            data = response.json()

            if not data.get("data"):
                return None

            tuple_data = data["data"]

            return CheckpointTuple(
                config=tuple_data["config"],
                checkpoint=tuple_data["checkpoint"],
                metadata=tuple_data["metadata"],
                parent_config=tuple_data.get("parent_config"),
            )

        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None
            raise

    @override
    async def alist(
        self,
        config: RunnableConfig | None = None,
        *,
        filter: dict[str, Any] | None = None,
        before: RunnableConfig | None = None,
        limit: int | None = None,
    ) -> AsyncIterator[CheckpointTuple]:
        """List checkpoints for a thread.

        Args:
            config: LangGraph runnable config
            filter: Optional filter criteria (not implemented)
            before: Return checkpoints before this config
            limit: Maximum number of checkpoints to return

        Yields:
            CheckpointTuple instances
        """
        if not config:
            raise ValueError("config is required")

        thread_id, user_id, checkpoint_ns = self._extract_config_values(config)

        try:
            # Prepare query parameters
            params = {
                "thread_id": thread_id,
                "checkpoint_ns": checkpoint_ns,
            }

            if before and "configurable" in before:
                before_checkpoint_id = before["configurable"].get("checkpoint_id")
                if before_checkpoint_id:
                    params["before_checkpoint_id"] = before_checkpoint_id

            if limit:
                params["limit"] = str(limit)

            # Call the Go service
            response = await self.client.get(
                "/api/langgraph/checkpoints", params=params, headers={"X-User-ID": user_id}
            )

            if response.status_code == 404:
                return

            response.raise_for_status()
            data = response.json()

            if not data.get("data"):
                return

            # Yield checkpoint tuples
            for tuple_data in data["data"]:
                yield CheckpointTuple(
                    config=tuple_data["config"],
                    checkpoint=tuple_data["checkpoint"],
                    metadata=tuple_data["metadata"],
                    parent_config=tuple_data.get("parent_config"),
                )

        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return
            raise

    # Synchronous methods (delegate to async versions)
    @override
    def put(
        self,
        config: RunnableConfig,
        checkpoint: Checkpoint,
        metadata: CheckpointMetadata,
        new_versions: dict[str, Any] | None = None,
    ) -> RunnableConfig:
        """Synchronous version of aput."""
        raise NotImplementedError("Use async version (aput) instead")

    @override
    def get_tuple(self, config: RunnableConfig) -> CheckpointTuple | None:
        """Synchronous version of aget_tuple."""
        raise NotImplementedError("Use async version (aget_tuple) instead")

    @override
    def list(
        self,
        config: RunnableConfig | None = None,
        *,
        filter: dict[str, Any] | None = None,
        before: RunnableConfig | None = None,
        limit: int | None = None,
    ) -> Iterator[CheckpointTuple]:
        """Synchronous version of alist."""
        raise NotImplementedError("Use async version (alist) instead")
