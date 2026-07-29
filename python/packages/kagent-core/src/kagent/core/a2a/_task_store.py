import asyncio
from typing import Any

import grpc
from a2a.server.tasks import TaskStore
from a2a.types import Artifact, Message, Task
from kagent.api.v1alpha1 import sessions_pb2
from typing_extensions import override

from kagent.core.a2a import read_metadata_value

from .._grpc import AsyncControllerClient
from .._structured_object import decode_structured_object, encode_structured_object

_A2A_API_VERSION = "lf.a2a.v1"
_A2A_TASK_KIND = "Task"
_GO_TASK_STATES = {
    "TASK_STATE_UNSPECIFIED": "unknown",
    "TASK_STATE_UNKNOWN": "unknown",
    "TASK_STATE_SUBMITTED": "submitted",
    "TASK_STATE_WORKING": "working",
    "TASK_STATE_INPUT_REQUIRED": "input-required",
    "TASK_STATE_COMPLETED": "completed",
    "TASK_STATE_CANCELED": "canceled",
    "TASK_STATE_CANCELLED": "canceled",
    "TASK_STATE_FAILED": "failed",
    "TASK_STATE_REJECTED": "rejected",
    "TASK_STATE_AUTH_REQUIRED": "auth-required",
}
_SDK_TASK_STATES = set(_GO_TASK_STATES.values())
_SDK_TO_GO_TASK_STATES = {
    "unknown": "TASK_STATE_UNSPECIFIED",
    "submitted": "TASK_STATE_SUBMITTED",
    "working": "TASK_STATE_WORKING",
    "input-required": "TASK_STATE_INPUT_REQUIRED",
    "completed": "TASK_STATE_COMPLETED",
    "canceled": "TASK_STATE_CANCELED",
    "failed": "TASK_STATE_FAILED",
    "rejected": "TASK_STATE_REJECTED",
    "auth-required": "TASK_STATE_AUTH_REQUIRED",
}


def _task_to_controller_payload(payload: dict[str, Any]) -> dict[str, Any]:
    normalized = {key: value for key, value in payload.items() if key != "kind"}
    status = _required_dict(payload.get("status"), "Task status")
    normalized["status"] = _status_to_controller_payload(status)

    if "history" in payload:
        history = payload["history"]
        if not isinstance(history, list):
            raise ValueError("Task history must be an array")
        normalized["history"] = [_message_to_controller_payload(message) for message in history]

    if "artifacts" in payload:
        artifacts = payload["artifacts"]
        if not isinstance(artifacts, list):
            raise ValueError("Task artifacts must be an array")
        normalized["artifacts"] = [_artifact_to_controller_payload(artifact) for artifact in artifacts]

    return normalized


def _status_to_controller_payload(value: dict[str, Any]) -> dict[str, Any]:
    normalized = dict(value)
    state = value.get("state")
    if not isinstance(state, str) or state not in _SDK_TO_GO_TASK_STATES:
        raise ValueError(f"Unsupported A2A task state: {state}")
    normalized["state"] = _SDK_TO_GO_TASK_STATES[state]
    if "message" in value:
        normalized["message"] = _message_to_controller_payload(value["message"])
    return normalized


def _message_to_controller_payload(value: Any) -> dict[str, Any]:
    message = _required_dict(value, "Task message")
    parts = message.get("parts")
    if not isinstance(parts, list):
        raise ValueError("Task message parts must be an array")

    normalized = {key: item for key, item in message.items() if key != "kind"}
    normalized["parts"] = [_part_to_controller_payload(part) for part in parts]
    role = message.get("role")
    if role == "user":
        normalized["role"] = "ROLE_USER"
    elif role == "agent":
        normalized["role"] = "ROLE_AGENT"
    else:
        raise ValueError(f"Unsupported A2A message role: {role}")
    return normalized


def _artifact_to_controller_payload(value: Any) -> dict[str, Any]:
    artifact = _required_dict(value, "Task artifact")
    parts = artifact.get("parts")
    if not isinstance(parts, list):
        raise ValueError("Task artifact parts must be an array")
    normalized = dict(artifact)
    normalized["parts"] = [_part_to_controller_payload(part) for part in parts]
    return normalized


def _part_to_controller_payload(value: Any) -> dict[str, Any]:
    part = _required_dict(value, "Task content part")
    kind = part.get("kind")
    if kind == "text":
        normalized = {"text": part.get("text")}
    elif kind == "data":
        normalized = {"data": part.get("data")}
    elif kind == "file":
        file = _required_dict(part.get("file"), "Task file part")
        has_uri = "uri" in file
        has_bytes = "bytes" in file
        if has_uri == has_bytes:
            raise ValueError("Task file part must have exactly one of uri or bytes")
        normalized = {"url" if has_uri else "raw": file["uri" if has_uri else "bytes"]}
        if name := file.get("name"):
            normalized["filename"] = name
        if mime_type := file.get("mimeType"):
            normalized["mediaType"] = mime_type
    else:
        raise ValueError(f"Unsupported A2A part kind: {kind}")

    if isinstance(part.get("metadata"), dict):
        normalized["metadata"] = part["metadata"]
    return normalized


def _task_from_controller_payload(payload: dict[str, Any]) -> dict[str, Any]:
    normalized = dict(payload)
    status = _required_dict(payload.get("status"), "Task status")
    normalized["kind"] = "task"
    normalized["status"] = _status_from_controller_payload(status)

    if "history" in payload:
        history = payload["history"]
        if not isinstance(history, list):
            raise ValueError("Task history must be an array")
        normalized["history"] = [_message_from_controller_payload(message) for message in history]

    if "artifacts" in payload:
        artifacts = payload["artifacts"]
        if not isinstance(artifacts, list):
            raise ValueError("Task artifacts must be an array")
        normalized["artifacts"] = [_artifact_from_controller_payload(artifact) for artifact in artifacts]

    return normalized


def _status_from_controller_payload(value: dict[str, Any]) -> dict[str, Any]:
    normalized = dict(value)
    state = value.get("state")
    if state in (None, ""):
        normalized["state"] = "unknown"
    elif isinstance(state, str) and state in _GO_TASK_STATES:
        normalized["state"] = _GO_TASK_STATES[state]
    elif isinstance(state, str) and state in _SDK_TASK_STATES:
        normalized["state"] = state
    else:
        raise ValueError(f"Unsupported A2A task state: {state}")

    if "message" in value:
        normalized["message"] = _message_from_controller_payload(value["message"])
    return normalized


def _message_from_controller_payload(value: Any) -> dict[str, Any]:
    message = _required_dict(value, "Task message")
    parts = message.get("parts")
    if not isinstance(parts, list):
        raise ValueError("Task message parts must be an array")

    normalized = dict(message)
    normalized["kind"] = "message"
    normalized["parts"] = [_part_from_controller_payload(part) for part in parts]
    role = message.get("role")
    if role in ("ROLE_USER", "user"):
        normalized["role"] = "user"
    elif role in ("ROLE_AGENT", "agent"):
        normalized["role"] = "agent"
    else:
        raise ValueError(f"Unsupported A2A message role: {role}")
    return normalized


def _artifact_from_controller_payload(value: Any) -> dict[str, Any]:
    artifact = _required_dict(value, "Task artifact")
    parts = artifact.get("parts")
    if not isinstance(parts, list):
        raise ValueError("Task artifact parts must be an array")
    normalized = dict(artifact)
    normalized["parts"] = [_part_from_controller_payload(part) for part in parts]
    return normalized


def _part_from_controller_payload(value: Any) -> dict[str, Any]:
    part = _required_dict(value, "Task content part")
    if part.get("kind") in {"text", "data", "file"}:
        return part

    content_fields = [field for field in ("text", "data", "url", "raw") if field in part]
    if len(content_fields) != 1:
        raise ValueError(f"Task content part must have exactly one content field; received {len(content_fields)}")

    content_field = content_fields[0]
    normalized: dict[str, Any]
    if content_field == "text":
        normalized = {"kind": "text", "text": part["text"]}
    elif content_field == "data":
        normalized = {"kind": "data", "data": part["data"]}
    else:
        file: dict[str, Any] = {}
        if filename := part.get("filename"):
            file["name"] = filename
        if media_type := part.get("mediaType"):
            file["mimeType"] = media_type
        if content_field == "url":
            file["uri"] = part["url"]
        else:
            file["bytes"] = part["raw"]
        normalized = {"kind": "file", "file": file}

    if isinstance(part.get("metadata"), dict):
        normalized["metadata"] = part["metadata"]
    return normalized


def _required_dict(value: Any, description: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{description} must be an object")
    return value


class KAgentTaskStore(TaskStore):
    """A task store that persists canonical A2A tasks via controller gRPC."""

    def __init__(self, client: AsyncControllerClient):
        self.client = client
        # Event-based sync: track pending save operations
        self._save_events: dict[str, asyncio.Event] = {}

    def _is_partial_event(self, item: Message) -> bool:
        """Check if a history item is a partial ADK streaming event."""
        metadata = item.metadata or {}
        return read_metadata_value(metadata, "partial") is True

    def _clean_partial_events(self, history: list[Message]) -> list[Message]:
        """Remove partial streaming events from history."""
        return [item for item in history if item.parts and not self._is_partial_event(item)]

    def _clean_partial_artifacts(self, artifacts: list[Artifact]) -> list[Artifact]:
        """Remove partial streaming artifacts."""
        return [artifact for artifact in artifacts if artifact.parts and not self._is_partial_event(artifact)]

    @override
    async def save(self, task: Task, context=None) -> None:
        """Save a task to KAgent.

        Skips saving if the current event is a partial streaming chunk.
        The adk_partial flag is set on event.metadata by AgentExecutor and
        gets copied to task.metadata by TaskManager.

        Args:
            task: The task to save
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        """
        persistent_task = task.model_copy(
            update={
                "history": self._clean_partial_events(task.history or []),
                "artifacts": self._clean_partial_artifacts(task.artifacts or []),
            }
        )

        payload = persistent_task.model_dump(mode="json", by_alias=True, exclude_none=True)
        encoded = encode_structured_object(
            _task_to_controller_payload(payload),
            api_version=_A2A_API_VERSION,
            kind=_A2A_TASK_KIND,
            max_bytes=self.client.max_message_bytes,
        )
        await self.client.task_service.CreateTask(
            sessions_pb2.CreateTaskRequest(task=encoded),
            **await self.client.call_options(),
        )

        # Signal that save completed (event-based sync)
        if task.id in self._save_events:
            self._save_events[task.id].set()

    @override
    async def get(self, task_id: str, context=None) -> Task | None:
        """Retrieve a task from KAgent.

        Args:
            task_id: The ID of the task to retrieve
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        Returns:
            The task if found, None otherwise

        """
        try:
            response = await self.client.task_service.GetTask(
                sessions_pb2.GetTaskRequest(task_id=task_id),
                **await self.client.call_options(),
            )
        except grpc.aio.AioRpcError as error:
            if error.code() == grpc.StatusCode.NOT_FOUND:
                return None
            raise
        payload = decode_structured_object(
            response.task,
            expected_kind=_A2A_TASK_KIND,
            max_bytes=self.client.max_message_bytes,
        )
        return Task.model_validate(_task_from_controller_payload(payload))

    @override
    async def delete(self, task_id: str, context=None) -> None:
        """Delete a task from KAgent.

        Args:
            task_id: The ID of the task to delete
            context: Server call context (unused, for a2a-sdk 0.3+ compatibility)

        """
        await self.client.task_service.DeleteTask(
            sessions_pb2.DeleteTaskRequest(task_id=task_id),
            **await self.client.call_options(),
        )

    async def wait_for_save(self, task_id: str, timeout: float = 5.0) -> None:
        """Wait for a task to be saved (event-based sync).

        This method is used to synchronize with the save operation instead of
        using arbitrary sleep delays. It's particularly useful after interrupts
        to ensure the task state is persisted before resuming.

        Args:
            task_id: The ID of the task to wait for
            timeout: Maximum time to wait in seconds (default: 5.0)

        Raises:
            asyncio.TimeoutError: If the save doesn't complete within timeout
        """
        event = asyncio.Event()
        self._save_events[task_id] = event
        try:
            await asyncio.wait_for(event.wait(), timeout=timeout)
        finally:
            # Clean up the event
            self._save_events.pop(task_id, None)
