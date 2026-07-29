"""KAgent remote checkpointer for LangGraph."""

import asyncio
import json
import logging
import random
from collections.abc import AsyncIterator, Awaitable, Callable, Iterator, Sequence
from typing import Any, TypeVar, cast

try:
    from typing import override  # Python 3.12+
except ImportError:
    from typing_extensions import override

import grpc
from kagent.api.v1alpha1 import langgraph_pb2
from kagent.core import AsyncControllerClient
from langchain_core.runnables import RunnableConfig

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

_TRANSIENT_STATUS_CODES = {
    grpc.StatusCode.DEADLINE_EXCEEDED,
    grpc.StatusCode.RESOURCE_EXHAUSTED,
    grpc.StatusCode.UNAVAILABLE,
}
_RETRY_ATTEMPTS = 3
_RETRY_DELAY_SECONDS = 0.5
ResponseT = TypeVar("ResponseT")


class KAgentCheckpointer(BaseCheckpointSaver[str]):
    """A remote checkpointer that stores LangGraph state in KAgent via the Go service.

    This checkpointer calls the KAgent Go HTTP service to persist graph state,
    enabling distributed execution and session recovery.
    """

    def __init__(
        self,
        client: AsyncControllerClient,
        app_name: str,
        serde: SerializerProtocol | None = None,
    ):
        """Initialize the checkpointer.

        Args:
            client: Shared generated gRPC client for the KAgent controller
            app_name: Application name (used for checkpoint namespace if not specified)
        """
        super().__init__(serde=serde)
        self.jsonplus_serde = JsonPlusSerializer()
        self.client = client
        self.app_name = app_name

    async def _call_with_retry(
        self,
        rpc: Callable[..., Awaitable[ResponseT]],
        request: Any,
        *,
        user_id: str,
        operation: str,
    ) -> ResponseT:
        last_error: grpc.aio.AioRpcError | None = None
        for attempt in range(_RETRY_ATTEMPTS):
            try:
                return await rpc(request, **await self.client.call_options(user_id))
            except asyncio.CancelledError:
                raise
            except grpc.aio.AioRpcError as error:
                if error.code() not in _TRANSIENT_STATUS_CODES:
                    raise
                last_error = error
                logger.warning(
                    "%s attempt %d/%d failed: %s",
                    operation,
                    attempt + 1,
                    _RETRY_ATTEMPTS,
                    error,
                )
                if attempt < _RETRY_ATTEMPTS - 1:
                    await asyncio.sleep(_RETRY_DELAY_SECONDS)

        assert last_error is not None
        logger.error("All %s attempts failed", operation, exc_info=last_error)
        raise last_error

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
        serialized_metadata = json.dumps(get_checkpoint_metadata(config, metadata)).encode()
        checkpoint_message = langgraph_pb2.LangGraphCheckpoint(
            thread_id=thread_id,
            checkpoint_ns=checkpoint_ns,
            checkpoint_id=checkpoint["id"],
            checkpoint=serialized_checkpoint,
            metadata=serialized_metadata,
            type=type_,
            version=checkpoint["v"],
        )
        parent_checkpoint_id = config.get("configurable", {}).get("checkpoint_id")
        if parent_checkpoint_id is not None:
            checkpoint_message.parent_checkpoint_id = parent_checkpoint_id

        # TODO: Deal with new_versions
        await self._call_with_retry(
            self.client.langgraph_service.PutCheckpoint,
            langgraph_pb2.PutCheckpointRequest(checkpoint=checkpoint_message),
            user_id=user_id,
            operation=f"checkpoint write for thread {thread_id}",
        )
        logger.debug("Stored checkpoint %s for thread %s", checkpoint["id"], thread_id)

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

        writes_data: list[langgraph_pb2.LangGraphCheckpointWrite] = []
        for idx, (channel, value) in enumerate(writes):
            type_, serialized_value = self.serde.dumps_typed(value)
            writes_data.append(
                langgraph_pb2.LangGraphCheckpointWrite(
                    idx=WRITES_IDX_MAP.get(channel, idx),
                    channel=channel,
                    type=type_,
                    value=serialized_value,
                )
            )

        await self._call_with_retry(
            self.client.langgraph_service.PutWrites,
            langgraph_pb2.PutWritesRequest(
                writes=langgraph_pb2.LangGraphCheckpointWrites(
                    thread_id=thread_id,
                    checkpoint_ns=checkpoint_ns,
                    checkpoint_id=checkpoint_id,
                    task_id=task_id,
                    writes=writes_data,
                )
            ),
            user_id=user_id,
            operation=f"checkpoint writes for thread {thread_id} checkpoint {checkpoint_id}",
        )
        logger.debug("Stored writes for checkpoint %s for thread %s", checkpoint_id, thread_id)

    def _convert_to_checkpoint_tuple(
        self,
        config: RunnableConfig,
        checkpoint_tuple: langgraph_pb2.LangGraphCheckpointTuple,
    ) -> CheckpointTuple:
        checkpoint = checkpoint_tuple.checkpoint
        return CheckpointTuple(
            config=config,
            checkpoint=self.serde.loads_typed((checkpoint.type, checkpoint.checkpoint)),
            metadata=cast(
                CheckpointMetadata,
                json.loads(checkpoint.metadata),
            ),
            parent_config=(
                {
                    "configurable": {
                        "thread_id": checkpoint.thread_id,
                        "checkpoint_ns": checkpoint.checkpoint_ns,
                        "checkpoint_id": checkpoint.parent_checkpoint_id,
                    }
                }
                if checkpoint.HasField("parent_checkpoint_id") and checkpoint.parent_checkpoint_id
                else None
            ),
            pending_writes=[
                PendingWrite(
                    (
                        write.task_id or checkpoint_tuple.writes.task_id,
                        write.channel,
                        self.serde.loads_typed((write.type, write.value)),
                    )
                )
                for write in checkpoint_tuple.writes.writes
            ],
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

        request = langgraph_pb2.ListCheckpointsRequest(
            thread_id=thread_id,
            checkpoint_ns=checkpoint_ns,
            limit=1,
        )
        if checkpoint_id := get_checkpoint_id(config):
            request.checkpoint_id = checkpoint_id

        try:
            response = await self.client.langgraph_service.ListCheckpoints(
                request,
                **await self.client.call_options(user_id),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.NOT_FOUND:
                return None
            raise

        if not response.checkpoints:
            return None

        checkpoint_tuple = response.checkpoints[0]

        if not checkpoint_id:
            config = {
                "configurable": {
                    "thread_id": thread_id,
                    "checkpoint_ns": checkpoint_ns,
                    "checkpoint_id": checkpoint_tuple.checkpoint.checkpoint_id,
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

        response = await self.client.langgraph_service.ListCheckpoints(
            langgraph_pb2.ListCheckpointsRequest(
                thread_id=thread_id,
                checkpoint_ns=checkpoint_ns,
                limit=limit if limit else -1,
            ),
            **await self.client.call_options(user_id),
        )
        for checkpoint_tuple in response.checkpoints:
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
