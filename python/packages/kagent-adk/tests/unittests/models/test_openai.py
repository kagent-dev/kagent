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
    return OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake")


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
    openai_llm = OpenAI(model="gpt-3.5-turbo", max_tokens=4096, type="openai", api_key="fake")
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


# ============================================================================
# SSL/TLS Configuration Tests
# ============================================================================


def test_openai_client_without_tls_config():
    """Test OpenAI client instantiation without TLS configuration (default behavior)."""
    openai_llm = OpenAI(model="gpt-3.5-turbo", type="openai", api_key="fake")
    client = openai_llm._client

    # Verify client is created
    assert client is not None
    # Default behavior should not have custom http_client
    # The _client property should use default httpx client


def test_openai_client_with_tls_verification_disabled():
    """Test OpenAI client with TLS verification disabled."""
    with mock.patch("kagent.adk.models._openai.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._openai.DefaultAsyncHttpxClient") as mock_httpx:
            with mock.patch("kagent.adk.models._openai.AsyncOpenAI") as mock_openai:
                # create_ssl_context returns False when verification is disabled
                mock_create_ssl.return_value = False
                mock_httpx_instance = mock.MagicMock()
                mock_httpx.return_value = mock_httpx_instance

                openai_llm = OpenAI(
                    model="gpt-3.5-turbo",
                    type="openai",
                    api_key="fake",
                    tls_disable_verify=True,
                )

                # Access _client to trigger httpx client creation
                _ = openai_llm._client

                # Verify create_ssl_context was called with correct parameters
                mock_create_ssl.assert_called_once_with(
                    disable_verify=True,
                    ca_cert_path=None,
                    disable_system_cas=False,
                )

                # Verify DefaultAsyncHttpxClient was created with verify=False
                mock_httpx.assert_called_once()
                call_kwargs = mock_httpx.call_args[1]
                assert call_kwargs["verify"] is False

                # Verify AsyncOpenAI was called with the http_client
                mock_openai.assert_called_once()
                openai_call_kwargs = mock_openai.call_args[1]
                assert openai_call_kwargs["http_client"] is mock_httpx_instance


def test_openai_client_with_custom_ca_certificate():
    """Test OpenAI client with custom CA certificate."""
    import ssl

    with mock.patch("kagent.adk.models._openai.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._openai.DefaultAsyncHttpxClient") as mock_httpx:
            with mock.patch("kagent.adk.models._openai.AsyncOpenAI"):
                # create_ssl_context returns SSLContext for custom CA
                mock_ssl_context = mock.MagicMock(spec=ssl.SSLContext)
                mock_create_ssl.return_value = mock_ssl_context
                mock_httpx_instance = mock.MagicMock()
                mock_httpx.return_value = mock_httpx_instance

                openai_llm = OpenAI(
                    model="gpt-3.5-turbo",
                    type="openai",
                    api_key="fake",
                    tls_ca_cert_path="/etc/ssl/certs/custom/ca.crt",
                    tls_disable_system_cas=False,
                )

                # Access _client to trigger httpx client creation
                _ = openai_llm._client

                # Verify create_ssl_context was called with correct parameters
                mock_create_ssl.assert_called_once_with(
                    disable_verify=False,
                    ca_cert_path="/etc/ssl/certs/custom/ca.crt",
                    disable_system_cas=False,
                )

                # Verify DefaultAsyncHttpxClient was created with SSL context
                mock_httpx.assert_called_once()
                call_kwargs = mock_httpx.call_args[1]
                assert call_kwargs["verify"] is mock_ssl_context


def test_openai_client_with_custom_ca_only():
    """Test OpenAI client with custom CA only (no system CAs)."""
    import ssl

    with mock.patch("kagent.adk.models._openai.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._openai.DefaultAsyncHttpxClient") as mock_httpx:
            with mock.patch("kagent.adk.models._openai.AsyncOpenAI"):
                mock_ssl_context = mock.MagicMock(spec=ssl.SSLContext)
                mock_create_ssl.return_value = mock_ssl_context
                mock_httpx_instance = mock.MagicMock()
                mock_httpx.return_value = mock_httpx_instance

                openai_llm = OpenAI(
                    model="gpt-3.5-turbo",
                    type="openai",
                    api_key="fake",
                    tls_ca_cert_path="/etc/ssl/certs/custom/ca.crt",
                    tls_disable_system_cas=True,
                )

                # Access _client to trigger httpx client creation
                _ = openai_llm._client

                # Verify create_ssl_context was called with disable_system_cas=True
                mock_create_ssl.assert_called_once_with(
                    disable_verify=False,
                    ca_cert_path="/etc/ssl/certs/custom/ca.crt",
                    disable_system_cas=True,
                )

                # Verify DefaultAsyncHttpxClient was created with SSL context
                mock_httpx.assert_called_once()
                call_kwargs = mock_httpx.call_args[1]
                assert call_kwargs["verify"] is mock_ssl_context


def test_openai_client_preserves_sdk_defaults():
    """Test that DefaultAsyncHttpxClient preserves OpenAI SDK defaults."""
    import ssl

    from openai import DefaultAsyncHttpxClient

    # Create a real DefaultAsyncHttpxClient with custom SSL context
    ssl_context = ssl.create_default_context()
    client = DefaultAsyncHttpxClient(verify=ssl_context)

    # Verify OpenAI defaults are preserved
    assert client.timeout.connect == 5.0
    assert client.timeout.read == 600
    assert client.timeout.write == 600
    assert client.timeout.pool == 600
    assert client.follow_redirects is True


def test_azure_openai_client_with_tls():
    """Test AzureOpenAI client uses DefaultAsyncHttpxClient with TLS configuration."""
    import ssl

    from kagent.adk.models import AzureOpenAI

    with mock.patch("kagent.adk.models._openai.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._openai.DefaultAsyncHttpxClient") as mock_httpx:
            with mock.patch("kagent.adk.models._openai.AsyncAzureOpenAI") as mock_azure_openai:
                mock_ssl_context = mock.MagicMock(spec=ssl.SSLContext)
                mock_create_ssl.return_value = mock_ssl_context
                mock_httpx_instance = mock.MagicMock()
                mock_httpx.return_value = mock_httpx_instance

                azure_llm = AzureOpenAI(
                    model="gpt-35-turbo",
                    type="azure_openai",
                    api_key="fake",
                    azure_endpoint="https://test.openai.azure.com",
                    api_version="2024-02-15-preview",
                    tls_ca_cert_path="/etc/ssl/certs/custom/ca.crt",
                )

                # Access _client to trigger client creation
                _ = azure_llm._client

                # Verify SSL context was created
                mock_create_ssl.assert_called_once_with(
                    disable_verify=False,
                    ca_cert_path="/etc/ssl/certs/custom/ca.crt",
                    disable_system_cas=False,
                )

                # Verify DefaultAsyncHttpxClient was created with SSL context
                mock_httpx.assert_called_once()
                call_kwargs = mock_httpx.call_args[1]
                assert call_kwargs["verify"] is mock_ssl_context

                # Verify AsyncAzureOpenAI was called with the http_client
                mock_azure_openai.assert_called_once()
                azure_call_kwargs = mock_azure_openai.call_args[1]
                assert azure_call_kwargs["http_client"] is mock_httpx_instance


def test_openai_client_with_base_url_and_tls():
    """Test OpenAI client with base_url (LiteLLM gateway) and TLS configuration."""
    import ssl

    with mock.patch("kagent.adk.models._openai.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._openai.DefaultAsyncHttpxClient") as mock_httpx:
            with mock.patch("kagent.adk.models._openai.AsyncOpenAI"):
                mock_ssl_context = mock.MagicMock(spec=ssl.SSLContext)
                mock_create_ssl.return_value = mock_ssl_context
                mock_httpx_instance = mock.MagicMock()
                mock_httpx.return_value = mock_httpx_instance

                openai_llm = OpenAI(
                    model="gpt-3.5-turbo",
                    type="openai",
                    api_key="fake",
                    base_url="https://litellm.internal.corp:8080",
                    tls_ca_cert_path="/etc/ssl/certs/custom/ca.crt",
                )

                # Access _client to trigger client creation
                _ = openai_llm._client

                # Verify SSL context was created
                mock_create_ssl.assert_called_once()

                # Verify DefaultAsyncHttpxClient was created with SSL context
                mock_httpx.assert_called_once()
