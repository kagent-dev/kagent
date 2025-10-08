import logging
from typing import Any, Dict, List

import httpx
from pydantic import BaseModel


class KagentMemoryPayload(BaseModel):
    thread_id: str
    user_id: str
    memory_data: Dict[str, Any]


class KagentMemoryResponse(BaseModel):
    data: List[KagentMemoryPayload]


class KagentMemoryStorage:
    """
    KagentMemoryStorage is a custom storage class for CrewAI's LongTermMemory.
    It persists memory items to the Kagent backend, scoped by thread_id and user_id.
    """

    def __init__(self, thread_id: str, user_id: str, base_url: str):
        self.thread_id = thread_id
        self.user_id = user_id
        self.base_url = base_url

    def save(self, task_description: str, metadata: dict, timestamp: str, score: float) -> None:
        """
        Saves a memory item to the Kagent backend.
        The agent_id is expected to be in the metadata.
        """
        url = f"{self.base_url}/api/crewai/memory"
        payload = KagentMemoryPayload(
            thread_id=self.thread_id,
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
            with httpx.Client() as client:
                response = client.post(url, json=payload.model_dump(), headers={"X-User-ID": self.user_id})
                response.raise_for_status()
        except httpx.HTTPError as e:
            logging.error(f"Error saving memory to Kagent backend: {e}")
            raise

    def load(self, task_description: str, latest_n: int) -> List[Dict[str, Any]] | None:
        """
        Loads memory items from the Kagent backend.
        Returns memory items matching the task description, up to latest_n items.
        """
        url = f"{self.base_url}/api/crewai/memory"
        # Use task_description as the query parameter to search across all agents for this session
        params = {"q": task_description, "limit": latest_n, "thread_id": self.thread_id}

        logging.debug(f"Loading memory from Kagent backend with params: {params}")
        try:
            with httpx.Client() as client:
                response = client.get(url, params=params, headers={"X-User-ID": self.user_id})
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

            return results if results else None
        except httpx.HTTPError as e:
            logging.error(f"Error loading memory from Kagent backend: {e}")
            return None

    def reset(self) -> None:
        """
        Resets the memory storage by deleting all memories for this session.
        """
        url = f"{self.base_url}/api/crewai/memory"
        params = {"thread_id": self.thread_id}

        logging.info(f"Resetting memory for session {self.thread_id}")
        try:
            with httpx.Client() as client:
                response = client.delete(url, params=params, headers={"X-User-ID": self.user_id})
                response.raise_for_status()
            logging.info(f"Successfully reset memory for session {self.thread_id}")
        except httpx.HTTPError as e:
            logging.error(f"Error resetting memory for session {self.thread_id}: {e}")
            raise
