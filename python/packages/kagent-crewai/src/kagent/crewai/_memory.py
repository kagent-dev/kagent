from typing import Any, Dict, List, Optional, Union
import logging
from datetime import datetime, timezone

import requests
from pydantic import BaseModel, Field

from crewai.flow.persistence import FlowPersistence


class KagentMemoryPayload(BaseModel):
    session_id: str = Field(..., alias="thread_id")
    user_id: str
    memory_data: Dict[str, Any]


class KagentMemoryResponse(BaseModel):
    data: List[KagentMemoryPayload]


class KagentFlowStatePayload(BaseModel):
    session_id: str = Field(..., alias="thread_id")
    flow_uuid: str
    method_name: str
    timestamp: str  # ISO format timestamp from when the state change happened
    state_data: Dict[str, Any]


class KagentFlowStateResponse(BaseModel):
    data: KagentFlowStatePayload


class KagentMemoryStorage:
    """
    KagentMemoryStorage is a custom storage class for CrewAI's LongTermMemory.
    It persists memory items to the Kagent backend, scoped by session_id and user_id.
    """

    def __init__(self, session_id: str, user_id: str, base_url: str = "http://localhost:8080"):
        self.session_id = session_id
        self.user_id = user_id
        self.base_url = base_url

    def save(self, task_description: str, metadata: dict, timestamp: str, score: float) -> None:
        """
        Saves a memory item to the Kagent backend.
        The agent_id is expected to be in the metadata.
        """
        url = f"{self.base_url}/api/crewai/memory"
        payload = KagentMemoryPayload(
            session_id=self.session_id,
            user_id=self.user_id,
            memory_data={
                "task_description": task_description,
                "score": score,
                "metadata": metadata,
                "datetime": timestamp,
            },
        )

        logging.info(f"Saving memory to Kagent backend: {payload}")

        try:
            response = requests.post(url, json=payload.model_dump(by_alias=True), headers={"X-User-ID": self.user_id})
            response.raise_for_status()
        except requests.exceptions.RequestException as e:
            logging.error(f"Error saving memory to Kagent backend: {e}")
            raise

    def load(self, task_description: str, latest_n: int) -> List[Dict[str, Any]] | None:
        """
        Loads memory items from the Kagent backend.
        Returns memory items matching the task description, up to latest_n items.
        """
        url = f"{self.base_url}/api/crewai/memory"
        # Use task_description as the query parameter to search across all agents for this session
        params = {"q": task_description, "limit": latest_n, "thread_id": self.session_id}

        logging.debug(f"Loading memory from Kagent backend with params: {params}")
        try:
            response = requests.get(url, params=params, headers={"X-User-ID": self.user_id})
            response.raise_for_status()

            # Parse response and convert to the format expected by the original interface
            memory_response = KagentMemoryResponse.model_validate_json(response.text)
            if not memory_response.data:
                return None

            # Convert to the format expected by LongTermMemory: list of dicts with metadata, datetime, score
            results = []
            for item in memory_response.data:
                memory_data = item.memory_data
                # The memory_data contains: task_description, score, metadata, datetime
                # We want to return items in the format that LongTermMemory expects
                results.append(
                    {
                        "metadata": memory_data.get("metadata", {}),
                        "datetime": memory_data.get("datetime", ""),
                        "score": memory_data.get("score", 0.0),
                    }
                )

            # Sort by datetime DESC, then by score ASC (matching SQLite behavior)
            results.sort(key=lambda x: (x["datetime"], x["score"]), reverse=True)

            return results if results else None
        except requests.exceptions.RequestException as e:
            logging.error(f"Error loading memory from Kagent backend: {e}")
            return None

    def reset(self) -> None:
        """
        Resets the memory storage by deleting all memories for this session.
        """
        url = f"{self.base_url}/api/crewai/memory"
        params = {"thread_id": self.session_id}

        logging.info(f"Resetting memory for session {self.session_id}")
        try:
            response = requests.delete(url, params=params, headers={"X-User-ID": self.user_id})
            response.raise_for_status()
            logging.info(f"Successfully reset memory for session {self.session_id}")
        except requests.exceptions.RequestException as e:
            logging.error(f"Error resetting memory for session {self.session_id}: {e}")
            raise


class KagentFlowPersistence(FlowPersistence):
    """
    KagentFlowPersistence is a custom persistence class for CrewAI Flows.
    It saves and loads the flow state to the Kagent backend.
    """

    def __init__(self, session_id: str, user_id: str, base_url: str = "http://localhost:8080"):
        self.session_id = session_id
        self.user_id = user_id
        self.base_url = base_url
        self.init_db()

    def init_db(self) -> None:
        # Nothing to do here as the backend handles DB initialization
        pass

    def save_state(self, flow_uuid: str, method_name: str, state_data: Union[Dict[str, Any], BaseModel]) -> None:
        """Saves the flow state to the Kagent backend."""
        url = f"{self.base_url}/api/crewai/flows/state"
        payload = KagentFlowStatePayload(
            session_id=self.session_id,
            flow_uuid=flow_uuid,
            method_name=method_name,
            timestamp=datetime.now(timezone.utc).isoformat(),
            state_data=state_data.model_dump() if isinstance(state_data, BaseModel) else state_data,
        )
        logging.info(f"Saving flow state to Kagent backend: {payload}")
        try:
            response = requests.post(url, json=payload.model_dump(by_alias=True), headers={"X-User-ID": self.user_id})
            response.raise_for_status()
        except requests.exceptions.RequestException as e:
            logging.error(f"Error saving flow state to Kagent backend: {e}")
            raise

    def load_state(self, flow_uuid: str) -> Optional[Dict[str, Any]]:
        """Loads the flow state from the Kagent backend."""
        url = f"{self.base_url}/api/crewai/flows/state"
        params = {"thread_id": self.session_id, "flow_uuid": flow_uuid}
        logging.info(f"Loading flow state from Kagent backend with params: {params}")
        try:
            response = requests.get(url, params=params, headers={"X-User-ID": self.user_id})
            if response.status_code == 404:
                return None
            response.raise_for_status()
            return KagentFlowStateResponse.model_validate_json(response.text).data.state_data
        except requests.exceptions.RequestException as e:
            logging.error(f"Error loading flow state from Kagent backend: {e}")
            return None
