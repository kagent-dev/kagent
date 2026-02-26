# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from google.genai import types as genai_types

from kagent.adk.converters.part_converter import convert_genai_part_to_a2a_part
from kagent.core.a2a import (
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
    get_kagent_metadata_key,
)


class TestPartConverterMetadataExtraction:
    """Test cases for convert_genai_part_to_a2a_part() metadata extraction."""

    def test_metadata_extracted_from_function_response(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:context_id": "ctx-123",
                "a2a:task_id": "task-456",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata is not None
        assert a2a_part.root.metadata["a2a:context_id"] == "ctx-123"
        assert a2a_part.root.metadata["a2a:task_id"] == "task-456"

        assert "a2a:context_id" not in a2a_part.root.data["response"]
        assert "a2a:task_id" not in a2a_part.root.data["response"]
        assert a2a_part.root.data["response"]["result"] == "ok"

    def test_metadata_only_context_id_extracted(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:context_id": "ctx-only",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata["a2a:context_id"] == "ctx-only"
        assert "a2a:task_id" not in a2a_part.root.metadata
        assert "a2a:context_id" not in a2a_part.root.data["response"]

    def test_metadata_only_task_id_extracted(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:task_id": "task-only",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata["a2a:task_id"] == "task-only"
        assert "a2a:context_id" not in a2a_part.root.metadata
        assert "a2a:task_id" not in a2a_part.root.data["response"]

    def test_no_metadata_when_not_present(self):
        """Test that DataPart metadata only has type key when no embedded metadata."""
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={"result": "ok"},
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.metadata is not None
        # Should only have the type key
        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY) in a2a_part.root.metadata
        assert (
            a2a_part.root.metadata[get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY)]
            == A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE
        )
        assert "a2a:context_id" not in a2a_part.root.metadata
        assert "a2a:task_id" not in a2a_part.root.metadata

    def test_metadata_keys_cleaned_from_response_dict(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "other_key": "other_value",
                "a2a:context_id": "ctx-123",
                "a2a:task_id": "task-456",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        response_data = a2a_part.root.data["response"]

        assert "a2a:context_id" not in response_data
        assert "a2a:task_id" not in response_data
        assert response_data["result"] == "ok"
        assert response_data["other_key"] == "other_value"

    def test_metadata_extraction_with_nested_response_dict(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "nested": {
                    "result": "ok",
                },
                "a2a:context_id": "ctx-nested",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.data["response"]["nested"]["result"] == "ok"
        assert "a2a:context_id" not in a2a_part.root.data["response"]
        assert a2a_part.root.metadata["a2a:context_id"] == "ctx-nested"

    def test_metadata_extraction_with_empty_response_dict(self):
        """Test that extraction handles empty response dict."""
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={},
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        assert a2a_part.root.data["response"] == {}
        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY) in a2a_part.root.metadata
        assert "a2a:context_id" not in a2a_part.root.metadata
        assert "a2a:task_id" not in a2a_part.root.metadata

    def test_metadata_extraction_preserves_other_metadata(self):
        function_response = genai_types.FunctionResponse(
            name="test_tool",
            id="call-1",
            response={
                "result": "ok",
                "a2a:context_id": "ctx-123",
            },
        )
        part = genai_types.Part(function_response=function_response)

        a2a_part = convert_genai_part_to_a2a_part(part)

        assert a2a_part is not None
        metadata = a2a_part.root.metadata

        assert get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY) in metadata
        assert (
            metadata[get_kagent_metadata_key(A2A_DATA_PART_METADATA_TYPE_KEY)]
            == A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE
        )
        assert metadata["a2a:context_id"] == "ctx-123"
