"""KAgent Remote Checkpointer for LangGraph.

This module implements a remote checkpointer that calls the KAgent Go service
for LangGraph checkpoint persistence via HTTP API.
"""

import base64
import logging
import random
from collections.abc import AsyncIterator, Iterator, Sequence
from typing import Any, cast, override

import httpx
from langchain_core.runnables import RunnableConfig
from pydantic import BaseModel

from langgraph.checkpoint.base import (
    WRITES_IDX_MAP,
    BaseCheckpointSaver,
    ChannelVersions,
    Checkpoint,
    CheckpointMetadata,
    CheckpointTuple,
    PendingWrite,
    get_checkpoint_id,
    get_checkpoint_metadata,
)
from langgraph.checkpoint.serde.base import SerializerProtocol
from langgraph.checkpoint.serde.jsonplus import JsonPlusSerializer

logger = logging.getLogger(__name__)


class KAgentCheckpointPayload(BaseModel):
    thread_id: str
    checkpoint_ns: str
    checkpoint_id: str
    parent_checkpoint_id: str | None = None
    checkpoint: str  # Serialized as UTF-8 string, not bytes
    metadata: str  # Serialized as UTF-8 string, not bytes
    type_: str
    version: int


class KagentCheckpointWrite(BaseModel):
    idx: int
    channel: str
    type_: str
    value: str  # Serialized as UTF-8 string, not bytes


class KAgentCheckpointWritePayload(BaseModel):
    thread_id: str
    checkpoint_ns: str
    checkpoint_id: str
    task_id: str
    writes: list[KagentCheckpointWrite]


class KAgentCheckpointTuple(BaseModel):
    thread_id: str
    checkpoint_ns: str
    checkpoint_id: str
    parent_checkpoint_id: str | None = None
    checkpoint: str  # Serialized as UTF-8 string, not bytes
    metadata: str  # Serialized as UTF-8 string, not bytes
    type_: str
    writes: KAgentCheckpointWritePayload | None = None


class KAgentCheckpointTupleResponse(BaseModel):
    data: list[KAgentCheckpointTuple] | None = None


class KAgentCheckpointer(BaseCheckpointSaver[str]):
    """A remote checkpointer that stores LangGraph state in KAgent via the Go service.

    This checkpointer calls the KAgent Go HTTP service to persist graph state,
    enabling distributed execution and session recovery.
    """

    def __init__(
        self,
        client: httpx.AsyncClient,
        app_name: str,
        serde: SerializerProtocol | None = None,
    ):
        """Initialize the checkpointer.

        Args:
            client: HTTP client configured with KAgent base URL
            app_name: Application name (used for checkpoint namespace if not specified)
        """
        super().__init__(serde=serde)
        self.jsonplus_serde = JsonPlusSerializer()
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
        new_versions: ChannelVersions,
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

        type_, serialized_checkpoint = self.serde.dumps_typed(checkpoint)
        serialized_metadata = self.jsonplus_serde.dumps(get_checkpoint_metadata(config, metadata))
        # Prepare request data
        request_data = KAgentCheckpointPayload(
            thread_id=thread_id,
            checkpoint_ns=checkpoint_ns,
            checkpoint_id=checkpoint["id"],
            parent_checkpoint_id=config.get("configurable", {}).get("checkpoint_id"),
            checkpoint=base64.b64encode(serialized_checkpoint).decode(
                "ascii"
            ),  # Base64 encode bytes to string for JSON serialization
            metadata=base64.b64encode(serialized_metadata).decode(
                "ascii"
            ),  # Base64 encode bytes to string for JSON serialization
            type_=type_,
            version=checkpoint["v"],
        )

        # TODO: Deal with new_versions

        # Call the Go service
        response = await self.client.post(
            "/api/langgraph/checkpoints",
            json=request_data.model_dump(),
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        logger.debug(f"Stored checkpoint {checkpoint['id']} for thread {thread_id}")

        return {
            "configurable": {
                "thread_id": thread_id,
                "checkpoint_ns": checkpoint_ns,
                "checkpoint_id": checkpoint["id"],
            }
        }

    @override
    async def aput_writes(
        self,
        config: RunnableConfig,
        writes: Sequence[tuple[str, Any]],
        task_id: str,
        task_path: str = "",
    ) -> None:
        """Store intermediate writes linked to a checkpoint."""
        thread_id, user_id, checkpoint_ns = self._extract_config_values(config)
        checkpoint_id = config.get("configurable", {}).get("checkpoint_id")
        if not checkpoint_id:
            raise ValueError("checkpoint_id is required in config.configurable for writing checkpoint data")

        writes_data = []
        for idx, (channel, value) in enumerate(writes):
            type_, serialized_value = self.serde.dumps_typed(value)
            writes_data.append(
                KagentCheckpointWrite(
                    idx=WRITES_IDX_MAP.get(channel, idx),
                    channel=channel,
                    type_=type_,
                    value=base64.b64encode(serialized_value).decode(
                        "ascii"
                    ),  # Base64 encode bytes to string for JSON serialization
                )
            )

        request_data = KAgentCheckpointWritePayload(
            thread_id=thread_id,
            checkpoint_ns=checkpoint_ns,
            checkpoint_id=checkpoint_id,
            task_id=task_id,
            writes=writes_data,
        )

        response = await self.client.post(
            "/api/langgraph/checkpoints/writes",
            json=request_data.model_dump(),
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        logger.debug(f"Stored writes for checkpoint {checkpoint_id} for thread {thread_id}")

    def _convert_to_checkpoint_tuple(
        self, config: RunnableConfig, checkpoint_tuple: KAgentCheckpointTuple
    ) -> CheckpointTuple:
        return CheckpointTuple(
            config=config,
            checkpoint=self.serde.loads_typed(
                (checkpoint_tuple.type_, base64.b64decode(checkpoint_tuple.checkpoint.encode("ascii")))
            ),
            metadata=cast(
                CheckpointMetadata,
                self.jsonplus_serde.loads(base64.b64decode(checkpoint_tuple.metadata.encode("ascii"))),
            ),
            parent_config=(
                {
                    "configurable": {
                        "thread_id": checkpoint_tuple.thread_id,
                        "checkpoint_ns": checkpoint_tuple.checkpoint_ns,
                        "checkpoint_id": checkpoint_tuple.parent_checkpoint_id,
                    }
                }
                if checkpoint_tuple.parent_checkpoint_id
                else None
            ),
            pending_writes=(
                [
                    PendingWrite(
                        (
                            checkpoint_tuple.writes.task_id,
                            write.channel,
                            self.serde.loads_typed((write.type_, base64.b64decode(write.value.encode("ascii")))),
                        )
                    )
                    for write in checkpoint_tuple.writes.writes
                ]
            )
            if checkpoint_tuple.writes
            else None,
        )

    @override
    async def aget_tuple(self, config: RunnableConfig) -> CheckpointTuple | None:
        """Retrieve the latest checkpoint for a thread.

        Args:
            config: LangGraph runnable config

        Returns:
            CheckpointTuple if found, None otherwise
        """
        thread_id, user_id, checkpoint_ns = self._extract_config_values(config)

        params = {"thread_id": thread_id, "checkpoint_ns": checkpoint_ns, "limit": "1"}
        if checkpoint_id := get_checkpoint_id(config):
            params["checkpoint_id"] = checkpoint_id

        response = await self.client.get(
            "/api/langgraph/checkpoints",
            params=params,
            headers={"X-User-ID": user_id},
        )
        if response.status_code == 404:
            return None

        response.raise_for_status()

        data = KAgentCheckpointTupleResponse.model_validate_json(response.text)

        if not data.data:
            return None

        checkpoint_tuple = data.data[0]

        if not checkpoint_id:
            config = {
                "configurable": {
                    "thread_id": thread_id,
                    "checkpoint_ns": checkpoint_ns,
                    "checkpoint_id": checkpoint_tuple.checkpoint_id,
                }
            }

        return self._convert_to_checkpoint_tuple(config, checkpoint_tuple)

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

        # Return all checkpoints if limit is not specified
        limit = limit if limit else -1

        response = await self.client.get(
            "/api/langgraph/checkpoints",
            params={"thread_id": thread_id, "checkpoint_ns": checkpoint_ns, "limit": str(limit)},
            headers={"X-User-ID": user_id},
        )
        response.raise_for_status()

        data = KAgentCheckpointTupleResponse.model_validate_json(response.text)

        if data.data:
            for checkpoint_tuple in data.data:
                yield self._convert_to_checkpoint_tuple(config, checkpoint_tuple)

    def get_next_version(self, current: str | None, channel: None) -> str:
        """Generate the next version ID for a channel.

        This method creates a new version identifier for a channel based on its current version.

        Args:
            current (Optional[str]): The current version identifier of the channel.

        Returns:
            str: The next version identifier, which is guaranteed to be monotonically increasing.
        """
        if current is None:
            current_v = 0
        elif isinstance(current, int):
            current_v = current
        else:
            current_v = int(current.split(".")[0])
        next_v = current_v + 1
        next_h = random.random()
        return f"{next_v:032}.{next_h:016}"

    # Synchronous methods (delegate to async versions)
    @override
    def put(
        self,
        config: RunnableConfig,
        checkpoint: Checkpoint,
        metadata: CheckpointMetadata,
        new_versions: ChannelVersions,
    ) -> RunnableConfig:
        """Synchronous version of aput."""
        raise NotImplementedError("Use async version (aput) instead")

    @override
    def put_writes(
        self,
        config: RunnableConfig,
        writes: Sequence[tuple[str, Any]],
        task_id: str,
        task_path: str = "",
    ) -> None:
        """Store intermediate writes linked to a checkpoint."""
        raise NotImplementedError("Not implemented")

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
