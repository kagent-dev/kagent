"""Unit tests for SAP AI Core model implementation."""

import pytest
from unittest.mock import AsyncMock, MagicMock, patch
from google.genai import types
from google.adk.models.llm_request import LlmRequest

from kagent.adk.models._sap_ai_core import SAPAICore


@pytest.fixture
def sap_ai_core_config():
    """Create a test SAP AI Core configuration."""
    return {
        "type": "sap_ai_core",
        "model": "gpt-4",
        "base_url": "https://api.ai.test.eu-central-1.aws.ml.hana.ondemand.com",
        "resource_group": "test-group",
        "deployment_id": "d123456789",
        "api_key": "test-api-key",
        "temperature": 0.7,
        "max_tokens": 100,
    }


@pytest.fixture
def sap_ai_core_llm(sap_ai_core_config):
    """Create a SAP AI Core LLM instance."""
    return SAPAICore(**sap_ai_core_config)


@pytest.fixture
def llm_request():
    """Create a test LLM request."""
    return LlmRequest(
        model="gpt-4",
        contents=[
            types.Content(
                role="user",
                parts=[types.Part.from_text(text="Hello, SAP AI Core!")]
            )
        ],
        config=types.GenerateContentConfig(
            temperature=0.7,
            response_modalities=[types.Modality.TEXT],
            system_instruction="You are a helpful assistant.",
        ),
    )


def test_sap_ai_core_initialization(sap_ai_core_llm, sap_ai_core_config):
    """Test SAP AI Core model initialization."""
    assert sap_ai_core_llm.model == sap_ai_core_config["model"]
    assert sap_ai_core_llm.base_url == sap_ai_core_config["base_url"]
    assert sap_ai_core_llm.resource_group == sap_ai_core_config["resource_group"]
    assert sap_ai_core_llm.deployment_id == sap_ai_core_config["deployment_id"]
    assert sap_ai_core_llm.temperature == sap_ai_core_config["temperature"]
    assert sap_ai_core_llm.max_tokens == sap_ai_core_config["max_tokens"]


def test_supported_models():
    """Test supported models regex."""
    models = SAPAICore.supported_models()
    assert len(models) > 0
    # SAP AI Core supports any model through deployments
    assert ".*" in models


def test_convert_content_to_messages(sap_ai_core_llm):
    """Test conversion of genai Content to SAP AI Core messages."""
    contents = [
        types.Content(
            role="user",
            parts=[types.Part.from_text(text="First message")]
        ),
        types.Content(
            role="model",
            parts=[types.Part.from_text(text="First response")]
        ),
        types.Content(
            role="user",
            parts=[types.Part.from_text(text="Second message")]
        ),
    ]
    
    messages = sap_ai_core_llm._convert_content_to_messages(
        contents,
        system_instruction="You are helpful."
    )
    
    assert len(messages) == 4  # 1 system + 3 messages
    assert messages[0]["role"] == "system"
    assert messages[0]["content"] == "You are helpful."
    assert messages[1]["role"] == "user"
    assert messages[1]["content"] == "First message"
    assert messages[2]["role"] == "assistant"
    assert messages[2]["content"] == "First response"
    assert messages[3]["role"] == "user"
    assert messages[3]["content"] == "Second message"


def test_convert_response_to_llm_response(sap_ai_core_llm):
    """Test conversion of SAP AI Core response to LlmResponse."""
    sap_response = {
        "choices": [
            {
                "message": {
                    "role": "assistant",
                    "content": "Hello! I'm working with SAP AI Core."
                },
                "finish_reason": "stop"
            }
        ],
        "usage": {
            "prompt_tokens": 10,
            "completion_tokens": 15,
            "total_tokens": 25
        }
    }
    
    llm_response = sap_ai_core_llm._convert_response_to_llm_response(sap_response)
    
    assert llm_response.content is not None
    assert llm_response.content.role == "model"
    assert len(llm_response.content.parts) == 1
    assert llm_response.content.parts[0].text == "Hello! I'm working with SAP AI Core."
    assert llm_response.usage_metadata is not None
    assert llm_response.usage_metadata.prompt_token_count == 10
    assert llm_response.usage_metadata.candidates_token_count == 15
    assert llm_response.usage_metadata.total_token_count == 25


@pytest.mark.asyncio
async def test_generate_content_async_success(sap_ai_core_llm, llm_request):
    """Test successful content generation."""
    mock_response = {
        "choices": [
            {
                "message": {
                    "role": "assistant",
                    "content": "Test response from SAP AI Core"
                },
                "finish_reason": "stop"
            }
        ],
        "usage": {
            "prompt_tokens": 5,
            "completion_tokens": 10,
            "total_tokens": 15
        }
    }
    
    with patch.object(sap_ai_core_llm, '_client') as mock_client:
        mock_post = AsyncMock()
        mock_post.return_value.json.return_value = mock_response
        mock_post.return_value.raise_for_status = MagicMock()
        mock_client.post = mock_post
        
        responses = []
        async for response in sap_ai_core_llm.generate_content_async(llm_request):
            responses.append(response)
        
        assert len(responses) == 1
        assert responses[0].content is not None
        assert "Test response from SAP AI Core" in responses[0].content.parts[0].text


@pytest.mark.asyncio
async def test_generate_content_async_with_oauth(sap_ai_core_llm, llm_request):
    """Test content generation with OAuth authentication."""
    sap_ai_core_llm.auth_url = "https://oauth.test.com/token"
    sap_ai_core_llm.client_id = "test-client-id"
    sap_ai_core_llm.client_secret = "test-client-secret"
    
    mock_oauth_response = {
        "access_token": "test-oauth-token",
        "token_type": "Bearer"
    }
    
    mock_response = {
        "choices": [
            {
                "message": {
                    "role": "assistant",
                    "content": "Response with OAuth"
                },
                "finish_reason": "stop"
            }
        ]
    }
    
    with patch('httpx.AsyncClient') as mock_async_client, \
         patch.object(sap_ai_core_llm, '_client') as mock_client:
        
        # Mock OAuth token request
        mock_oauth_client = AsyncMock()
        mock_oauth_post = AsyncMock()
        mock_oauth_post.return_value.json.return_value = mock_oauth_response
        mock_oauth_post.return_value.raise_for_status = MagicMock()
        mock_oauth_client.post = mock_oauth_post
        mock_async_client.return_value.__aenter__.return_value = mock_oauth_client
        
        # Mock API request
        mock_post = AsyncMock()
        mock_post.return_value.json.return_value = mock_response
        mock_post.return_value.raise_for_status = MagicMock()
        mock_client.post = mock_post
        
        responses = []
        async for response in sap_ai_core_llm.generate_content_async(llm_request):
            responses.append(response)
        
        assert len(responses) == 1
        # Verify OAuth token was requested
        mock_oauth_post.assert_called_once()


@pytest.mark.asyncio
async def test_generate_content_async_error(sap_ai_core_llm, llm_request):
    """Test error handling in content generation."""
    with patch.object(sap_ai_core_llm, '_client') as mock_client:
        mock_post = AsyncMock()
        mock_post.side_effect = Exception("API Error")
        mock_client.post = mock_post
        
        responses = []
        async for response in sap_ai_core_llm.generate_content_async(llm_request):
            responses.append(response)
        
        assert len(responses) == 1
        assert responses[0].error_code == "API_ERROR"
        assert "API Error" in responses[0].error_message


def test_client_headers(sap_ai_core_llm):
    """Test that client is configured with correct headers."""
    client = sap_ai_core_llm._client
    
    assert "Authorization" in client.headers
    assert client.headers["Authorization"] == "Bearer test-api-key"
    assert client.headers["AI-Resource-Group"] == "test-group"
    assert client.headers["Content-Type"] == "application/json"


def test_request_payload_construction(sap_ai_core_llm):
    """Test request payload construction with various parameters."""
    # This would test the actual payload structure if we exposed it
    # For now, we verify parameters are set correctly
    assert sap_ai_core_llm.temperature == 0.7
    assert sap_ai_core_llm.max_tokens == 100
    assert sap_ai_core_llm.deployment_id == "d123456789"



