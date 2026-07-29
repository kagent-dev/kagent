import asyncio
import logging
from collections.abc import Coroutine
from typing import Any, TypeVar

from kagent.api.v1alpha1 import crewai_pb2
from kagent.core import AsyncControllerClient, decode_structured_object, encode_structured_object

logger = logging.getLogger(__name__)

_API_VERSION = "kagent.api/v1alpha1"
_MEMORY_DATA_KIND = "CrewAIMemoryData"
ResultT = TypeVar("ResultT")


def _run_on_loop(coroutine: Coroutine[Any, Any, ResultT], loop: asyncio.AbstractEventLoop) -> ResultT:
    try:
        if asyncio.get_running_loop() is loop:
            coroutine.close()
            raise RuntimeError("CrewAI synchronous memory storage cannot run on the controller event loop")
    except RuntimeError as error:
        if str(error) != "no running event loop":
            raise
    return asyncio.run_coroutine_threadsafe(coroutine, loop).result()


class KagentMemoryStorage:
    """
    KagentMemoryStorage is a custom storage class for CrewAI's LongTermMemory.
    It persists memory items to the Kagent backend, scoped by thread_id and user_id.
    """

    def __init__(
        self,
        thread_id: str,
        user_id: str,
        client: AsyncControllerClient,
        loop: asyncio.AbstractEventLoop,
    ):
        self.thread_id = thread_id
        self.user_id = user_id
        self.client = client
        self.loop = loop

    async def _store_memory(self, memory_data: dict[str, Any]) -> None:
        await self.client.crewai_service.StoreMemory(
            crewai_pb2.StoreMemoryRequest(
                thread_id=self.thread_id,
                memory_data=encode_structured_object(
                    memory_data,
                    api_version=_API_VERSION,
                    kind=_MEMORY_DATA_KIND,
                    max_bytes=self.client.max_message_bytes,
                ),
            ),
            **await self.client.call_options(self.user_id),
        )

    def save(self, task_description: str, metadata: dict, timestamp: str, score: float) -> None:
        """
        Saves a memory item to the Kagent backend.
        The agent_id is expected to be in the metadata.
        """
        memory_data = {
            "task_description": task_description,
            "score": score,
            "metadata": metadata,
            "datetime": timestamp,
        }
        logger.info("Saving memory to KAgent backend for thread %s", self.thread_id)
        _run_on_loop(self._store_memory(memory_data), self.loop)

    async def _get_memory(self, task_description: str, latest_n: int) -> crewai_pb2.GetMemoryResponse:
        return await self.client.crewai_service.GetMemory(
            crewai_pb2.GetMemoryRequest(
                thread_id=self.thread_id,
                task_description=task_description,
                limit=latest_n,
            ),
            **await self.client.call_options(self.user_id),
        )

    def load(self, task_description: str, latest_n: int) -> list[dict[str, Any]] | None:
        """
        Loads memory items from the Kagent backend.
        Returns memory items matching the task description, up to latest_n items.
        """
        logger.debug("Loading memory from KAgent backend for thread %s", self.thread_id)
        try:
            response = _run_on_loop(self._get_memory(task_description, latest_n), self.loop)
            if not response.memories:
                return None

            results = []
            for item in response.memories:
                memory_data = decode_structured_object(
                    item.memory_data,
                    expected_kind=_MEMORY_DATA_KIND,
                    max_bytes=self.client.max_message_bytes,
                )
                results.append(
                    {
                        "metadata": memory_data.get("metadata", {}),
                        "datetime": memory_data.get("datetime", ""),
                        "score": memory_data.get("score", 0.0),
                    }
                )

            return results if results else None
        except Exception:
            logger.exception("Error loading memory from KAgent backend")
            return None

    async def _reset_memory(self) -> None:
        await self.client.crewai_service.ResetMemory(
            crewai_pb2.ResetMemoryRequest(thread_id=self.thread_id),
            **await self.client.call_options(self.user_id),
        )

    def reset(self) -> None:
        """
        Resets the memory storage by deleting all memories for this session.
        """
        logger.info("Resetting memory for session %s", self.thread_id)
        _run_on_loop(self._reset_memory(), self.loop)
        logger.info("Successfully reset memory for session %s", self.thread_id)
