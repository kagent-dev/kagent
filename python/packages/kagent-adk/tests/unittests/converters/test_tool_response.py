import pytest
from google.genai import types as genai_types
from google.protobuf.json_format import MessageToDict

from kagent.adk.converters.part_converter import convert_a2a_part_to_genai_part, convert_genai_part_to_a2a_part
from kagent.adk.converters.tool_response import encode_tool_response


@pytest.mark.parametrize(
    ("response", "expected"),
    [
        ({"output": {"structuredContent": {"ready": True}}}, {"output": {"ready": True}}),
        (
            {"output": {"structuredContent": None, "content": [{"type": "text", "text": "namespace active"}]}},
            {"output": "namespace active"},
        ),
        ({"output": {"isError": True, "content": [{"type": "text", "text": "not found"}]}}, {"error": "not found"}),
        ({"error": "confirmation required"}, {"error": "confirmation required"}),
        (
            {"status": "pending", "questions": ["namespace?"]},
            {"pending": {"status": "pending", "questions": ["namespace?"]}},
        ),
        ({"memories": []}, {"output": {"memories": []}}),
        ({"output": False}, {"output": False}),
        ({"output": 0}, {"output": 0}),
        ({"output": None}, {"output": None}),
    ],
)
def test_canonical_tool_response(response, expected):
    assert MessageToDict(encode_tool_response(response)) == expected


def test_function_response_round_trip_preserves_native_payload():
    part = genai_types.Part(
        function_response=genai_types.FunctionResponse(
            id="call", name="lookup", response={"result": "application data"}
        )
    )
    encoded = convert_genai_part_to_a2a_part(part)
    assert MessageToDict(encoded.data) == {
        "id": "call",
        "name": "lookup",
        "response": {"output": {"result": "application data"}},
    }
    decoded = convert_a2a_part_to_genai_part(encoded)
    assert decoded.function_response == part.function_response
