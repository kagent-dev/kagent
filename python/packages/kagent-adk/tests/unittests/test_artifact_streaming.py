from a2a.types import Artifact, Part, TaskArtifactUpdateEvent
from google.protobuf.json_format import ParseDict
from google.protobuf.struct_pb2 import Value

from kagent.adk._agent_executor import _split_hitl_artifact_parts
from kagent.core.a2a import get_kagent_metadata_key


def test_split_hitl_keeps_text_artifact_and_collects_long_running_parts():
    hitl_part = Part(data=ParseDict({"id": "call-1", "name": "dangerous_tool"}, Value()))
    hitl_part.metadata.update(
        {
            get_kagent_metadata_key("type"): "function_call",
            get_kagent_metadata_key("is_long_running"): True,
        }
    )
    event = TaskArtifactUpdateEvent(
        task_id="task-1",
        context_id="context-1",
        last_chunk=True,
        artifact=Artifact(
            artifact_id="artifact-1",
            parts=[Part(text="please confirm"), hitl_part],
        ),
    )
    hitl_parts: list[Part] = []

    kept = _split_hitl_artifact_parts(event, hitl_parts)

    assert kept is event
    assert [part.text for part in kept.artifact.parts] == ["please confirm"]
    assert len(hitl_parts) == 1
    assert hitl_parts[0].HasField("data")
    assert hitl_parts[0].data == hitl_part.data


def test_split_hitl_drops_artifact_when_only_long_running_parts_remain():
    hitl_part = Part(data=ParseDict({"id": "call-1", "name": "adk_request_confirmation"}, Value()))
    hitl_part.metadata.update({get_kagent_metadata_key("is_long_running"): True})
    event = TaskArtifactUpdateEvent(
        task_id="task-1",
        context_id="context-1",
        last_chunk=True,
        artifact=Artifact(artifact_id="artifact-1", parts=[hitl_part]),
    )
    hitl_parts: list[Part] = []

    assert _split_hitl_artifact_parts(event, hitl_parts) is None
    assert hitl_parts == [hitl_part]
