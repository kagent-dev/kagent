import logging
from typing import Any, Dict, Optional, Union

import httpx
from pydantic import BaseModel, Field

from crewai.flow.persistence import FlowPersistence


class KagentFlowStatePayload(BaseModel):
    thread_id: str
    flow_uuid: str
    method_name: str
    state_data: Dict[str, Any]


class KagentFlowStateResponse(BaseModel):
    data: KagentFlowStatePayload


class KagentFlowPersistence(FlowPersistence):
    """
    KagentFlowPersistence is a custom persistence class for CrewAI Flows.
    It saves and loads the flow state to the Kagent backend.
    """

    def __init__(self, thread_id: str, user_id: str, base_url: str):
        self.thread_id = thread_id
        self.user_id = user_id
        self.base_url = base_url

    def init_db(self) -> None:
        """This is handled by the Kagent backend, so no action is needed here."""
        pass

    def save_state(self, flow_uuid: str, method_name: str, state_data: Union[Dict[str, Any], BaseModel]) -> None:
        """Saves the flow state to the Kagent backend."""
        url = f"{self.base_url}/api/crewai/flows/state"
        payload = KagentFlowStatePayload(
            thread_id=self.thread_id,
            flow_uuid=flow_uuid,
            method_name=method_name,
            state_data=state_data.model_dump() if isinstance(state_data, BaseModel) else state_data,
        )
        logging.info(f"Saving flow state to Kagent backend: {payload}")

        try:
            with httpx.Client() as client:
                response = client.post(url, json=payload.model_dump(), headers={"X-User-ID": self.user_id})
                response.raise_for_status()
        except httpx.HTTPError as e:
            logging.error(f"Error saving flow state to Kagent backend: {e}")
            raise

    def load_state(self, flow_uuid: str) -> Optional[Dict[str, Any]]:
        """Loads the flow state from the Kagent backend."""
        url = f"{self.base_url}/api/crewai/flows/state"
        params = {"thread_id": self.thread_id, "flow_uuid": flow_uuid}
        logging.info(f"Loading flow state from Kagent backend with params: {params}")

        try:
            with httpx.Client() as client:
                response = client.get(url, params=params, headers={"X-User-ID": self.user_id})
            if response.status_code == 404:
                return None
            response.raise_for_status()
            return KagentFlowStateResponse.model_validate_json(response.text).data.state_data
        except httpx.HTTPError as e:
            logging.error(f"Error loading flow state from Kagent backend: {e}")
            return None
