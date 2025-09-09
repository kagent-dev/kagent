# Copyright 2025 Google LLC
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

from unittest import mock

import pytest
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai import types
from google.genai.types import Content, Part
from openai.types.chat.chat_completion_tool_param import ChatCompletionToolParam

from kagent.adk.models import OpenAI
from kagent.adk.models._openai import _convert_tools_to_openai


@pytest.fixture
def generate_content_response():
    # Create a mock response object
    class MockUsage:
        def __init__(self):
            self.completion_tokens = 12
            self.prompt_tokens = 13
            self.total_tokens = 25

    class MockMessage:
        def __init__(self):
            self.content = "Hi! How can I help you today?"
            self.role = "assistant"

    class MockChoice:
        def __init__(self):
            self.finish_reason = "stop"
            self.index = 0
            self.message = MockMessage()

    class MockResponse:
        def __init__(self):
            self.id = "chatcmpl-testid"
            self.choices = [MockChoice()]
            self.created = 1234567890
            self.model = "gpt-3.5-turbo"
            self.object = "chat.completion"
            self.usage = MockUsage()

    return MockResponse()


@pytest.fixture
def generate_llm_response():
    return LlmResponse.create(
        types.GenerateContentResponse(
            candidates=[
                types.Candidate(
                    content=Content(
                        role="model",
                        parts=[Part.from_text(text="Hello, how can I help you?")],
                    ),
                    finish_reason=types.FinishReason.STOP,
                )
            ]
        )
    )


@pytest.fixture
def openai_llm():
    return OpenAI(model="gpt-3.5-turbo", type="openai")


@pytest.fixture
def llm_request():
    return LlmRequest(
        model="gpt-3.5-turbo",
        contents=[Content(role="user", parts=[Part.from_text(text="Hello")])],
        config=types.GenerateContentConfig(
            temperature=0.1,
            response_modalities=[types.Modality.TEXT],
            system_instruction="You are a helpful assistant",
        ),
    )


def test_supported_models():
    models = OpenAI.supported_models()
    assert len(models) == 2
    assert models[0] == r"gpt-.*"
    assert models[1] == r"o1-.*"


function_declaration_test_cases = [
    (
        "function_with_no_parameters",
        types.FunctionDeclaration(
            name="get_current_time",
            description="Gets the current time.",
        ),
        ChatCompletionToolParam(
            type="function",
            function={
                "name": "get_current_time",
                "description": "Gets the current time.",
                "parameters": {"type": "object", "properties": {}, "required": []},
            },
        ),
    ),
    (
        "function_with_one_optional_parameter",
        types.FunctionDeclaration(
            name="get_weather",
            description="Gets weather information for a given location.",
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "location": types.Schema(
                        type=types.Type.STRING,
                        description="City and state, e.g., San Francisco, CA",
                    )
                },
            ),
        ),
        ChatCompletionToolParam(
            type="function",
            function={
                "name": "get_weather",
                "description": "Gets weather information for a given location.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "location": {
                            "type": "string",
                            "description": "City and state, e.g., San Francisco, CA",
                        }
                    },
                    "required": [],
                },
            },
        ),
    ),
    (
        "function_with_one_required_parameter",
        types.FunctionDeclaration(
            name="get_stock_price",
            description="Gets the current price for a stock ticker.",
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "ticker": types.Schema(
                        type=types.Type.STRING,
                        description="The stock ticker, e.g., AAPL",
                    )
                },
                required=["ticker"],
            ),
        ),
        ChatCompletionToolParam(
            type="function",
            function={
                "name": "get_stock_price",
                "description": "Gets the current price for a stock ticker.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "ticker": {
                            "type": "string",
                            "description": "The stock ticker, e.g., AAPL",
                        }
                    },
                    "required": ["ticker"],
                },
            },
        ),
    ),
    (
        "function_with_multiple_mixed_parameters",
        types.FunctionDeclaration(
            name="submit_order",
            description="Submits a product order.",
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "product_id": types.Schema(type=types.Type.STRING, description="The product ID"),
                    "quantity": types.Schema(
                        type=types.Type.INTEGER,
                        description="The order quantity",
                    ),
                    "notes": types.Schema(
                        type=types.Type.STRING,
                        description="Optional order notes",
                    ),
                },
                required=["product_id", "quantity"],
            ),
        ),
        ChatCompletionToolParam(
            type="function",
            function={
                "name": "submit_order",
                "description": "Submits a product order.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "product_id": {
                            "type": "string",
                            "description": "The product ID",
                        },
                        "quantity": {
                            "type": "integer",
                            "description": "The order quantity",
                        },
                        "notes": {
                            "type": "string",
                            "description": "Optional order notes",
                        },
                    },
                    "required": ["product_id", "quantity"],
                },
            },
        ),
    ),
    (
        "function_with_complex_nested_parameter",
        types.FunctionDeclaration(
            name="create_playlist",
            description="Creates a playlist from a list of songs.",
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "playlist_name": types.Schema(
                        type=types.Type.STRING,
                        description="The name for the new playlist",
                    ),
                    "songs": types.Schema(
                        type=types.Type.ARRAY,
                        description="A list of songs to add to the playlist",
                        items=types.Schema(
                            type=types.Type.OBJECT,
                            properties={
                                "title": types.Schema(type=types.Type.STRING),
                                "artist": types.Schema(type=types.Type.STRING),
                            },
                            required=["title", "artist"],
                        ),
                    ),
                },
                required=["playlist_name", "songs"],
            ),
        ),
        ChatCompletionToolParam(
            type="function",
            function={
                "name": "create_playlist",
                "description": "Creates a playlist from a list of songs.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "playlist_name": {
                            "type": "string",
                            "description": "The name for the new playlist",
                        },
                        "songs": {
                            "type": "array",
                            "description": "A list of songs to add to the playlist",
                            "items": {
                                "type": "object",
                                "properties": {
                                    "title": {"type": "string"},
                                    "artist": {"type": "string"},
                                },
                                "required": ["title", "artist"],
                            },
                        },
                    },
                    "required": ["playlist_name", "songs"],
                },
            },
        ),
    ),
]


@pytest.mark.parametrize(
    "_, function_declaration, expected_tool_param",
    function_declaration_test_cases,
    ids=[case[0] for case in function_declaration_test_cases],
)
async def test_function_declaration_to_tool_param(_, function_declaration, expected_tool_param):
    """Test _convert_tools_to_openai function."""
    tool = types.Tool(function_declarations=[function_declaration])
    result = _convert_tools_to_openai([tool])
    assert len(result) == 1
    assert result[0] == expected_tool_param


@pytest.mark.asyncio
async def test_generate_content_async(openai_llm, llm_request, generate_content_response, generate_llm_response):
    with mock.patch.object(openai_llm, "_client") as mock_client:
        # Create a mock coroutine that returns the generate_content_response.
        async def mock_coro(*args, **kwargs):
            return generate_content_response

        # Assign the coroutine to the mocked method
        mock_client.chat.completions.create.return_value = mock_coro()

        responses = [resp async for resp in openai_llm.generate_content_async(llm_request, stream=False)]
        assert len(responses) == 1
        assert isinstance(responses[0], LlmResponse)
        assert responses[0].content is not None
        assert len(responses[0].content.parts) > 0
        assert responses[0].content.parts[0].text == "Hi! How can I help you today?"


@pytest.mark.asyncio
async def test_generate_content_async_with_max_tokens(llm_request, generate_content_response, generate_llm_response):
    openai_llm = OpenAI(model="gpt-3.5-turbo", max_tokens=4096, type="openai")
    with mock.patch.object(openai_llm, "_client") as mock_client:
        # Create a mock coroutine that returns the generate_content_response.
        async def mock_coro(*args, **kwargs):
            return generate_content_response

        # Assign the coroutine to the mocked method
        mock_client.chat.completions.create.return_value = mock_coro()

        _ = [resp async for resp in openai_llm.generate_content_async(llm_request, stream=False)]
        mock_client.chat.completions.create.assert_called_once()
        _, kwargs = mock_client.chat.completions.create.call_args
        assert kwargs["max_tokens"] == 4096
