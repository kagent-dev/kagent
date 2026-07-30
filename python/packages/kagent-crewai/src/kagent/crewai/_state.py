import asyncio
import logging
from copy import deepcopy
from typing import Any, Union

import grpc
from kagent.api.v1alpha1 import crewai_pb2
from kagent.core import AsyncControllerClient, decode_structured_object, encode_structured_object
from pydantic import BaseModel

from crewai.flow.persistence import FlowPersistence

logger = logging.getLogger(__name__)

_API_VERSION = "kagent.api/v1alpha1"
_FLOW_STATE_DATA_KIND = "CrewAIFlowStateData"


class KagentFlowPersistence(FlowPersistence):
    """
    KagentFlowPersistence is a custom persistence class for CrewAI Flows.
    It saves and loads the flow state to the Kagent backend.
    """

    def __init__(
        self,
        thread_id: str,
        user_id: str,
        client: AsyncControllerClient,
        loaded_state: dict[str, Any] | None,
    ):
        self.thread_id = thread_id
        self.user_id = user_id
        self.client = client
        self._loaded_state = loaded_state
        self._loop = asyncio.get_running_loop()
        self._pending_write: asyncio.Task[None] | None = None

    @classmethod
    async def create(
        cls,
        thread_id: str,
        user_id: str,
        client: AsyncControllerClient,
    ) -> "KagentFlowPersistence":
        loaded_state = None
        try:
            response = await client.crewai_service.GetFlowState(
                crewai_pb2.GetFlowStateRequest(thread_id=thread_id),
                **await client.call_options(user_id),
            )
            loaded_state = decode_structured_object(
                response.state.state_data,
                expected_kind=_FLOW_STATE_DATA_KIND,
                max_bytes=client.max_message_bytes,
            )
        except grpc.aio.AioRpcError as error:
            if error.code() != grpc.StatusCode.NOT_FOUND:
                raise
        return cls(thread_id, user_id, client, loaded_state)

    def init_db(self) -> None:
        """This is handled by the Kagent backend, so no action is needed here."""
        pass

    def save_state(self, flow_uuid: str, method_name: str, state_data: Union[dict[str, Any], BaseModel]) -> None:
        """Saves the flow state to the Kagent backend."""
        if asyncio.get_running_loop() is not self._loop:
            raise RuntimeError("CrewAI flow persistence must run on the controller event loop")

        serialized_state = (
            state_data.model_dump(mode="json") if isinstance(state_data, BaseModel) else deepcopy(state_data)
        )
        self._loaded_state = serialized_state
        request = crewai_pb2.StoreFlowStateRequest(
            thread_id=self.thread_id,
            method_name=method_name,
            state_data=encode_structured_object(
                serialized_state,
                api_version=_API_VERSION,
                kind=_FLOW_STATE_DATA_KIND,
                max_bytes=self.client.max_message_bytes,
            ),
        )
        previous_write = self._pending_write

        async def store() -> None:
            if previous_write is not None:
                await previous_write
            await self.client.crewai_service.StoreFlowState(
                request,
                **await self.client.call_options(self.user_id),
            )

        logger.info("Saving flow state to KAgent backend for thread %s", self.thread_id)
        self._pending_write = self._loop.create_task(store())

    def load_state(self, flow_uuid: str) -> dict[str, Any] | None:
        """Loads the flow state from the Kagent backend."""
        return deepcopy(self._loaded_state)

    async def flush(self) -> None:
        if self._pending_write is None:
            return
        pending_write = self._pending_write
        await pending_write
        if self._pending_write is pending_write:
            self._pending_write = None
