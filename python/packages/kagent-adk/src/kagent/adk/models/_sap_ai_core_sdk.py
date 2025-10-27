"""SAP AI Core model implementation using official SDK for KAgent."""

from __future__ import annotations

import json
import os
from functools import cached_property
from typing import TYPE_CHECKING, AsyncGenerator, Literal, Optional

from google.adk.models import BaseLlm
from google.adk.models.llm_response import LlmResponse
from google.genai import types
from pydantic import Field

# SAP AI SDK imports
try:
    from sap.ai.core import AICoreClient
    from sap.ai.core.models import ChatCompletionRequest, ChatMessage
    from sap.ai.core.exceptions import AICoreException
    SAP_AI_SDK_AVAILABLE = True
except ImportError:
    SAP_AI_SDK_AVAILABLE = False
    AICoreClient = None
    ChatCompletionRequest = None
    ChatMessage = None
    AICoreException = Exception

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest


class SAPAICoreSDK(BaseLlm):
    """SAP AI Core model implementation using official SDK.
    
    This adapter uses the official SAP AI SDK (sap-ai-sdk-gen) to call 
    SAP AI Core generative AI deployments. It provides better error handling,
    automatic retries, and type safety compared to direct HTTP calls.
    
    Attributes:
        model: The model identifier (e.g., "gpt-4", "claude-3")
        base_url: SAP AI Core inference API base URL
        resource_group: SAP AI Core resource group
        deployment_id: SAP AI Core deployment ID
        api_key: Optional API key (or use SAP_AI_CORE_API_KEY env var)
        auth_url: OAuth token endpoint URL
        client_id: OAuth client ID
        client_secret: OAuth client secret (or use SAP_AI_CORE_CLIENT_SECRET env var)
        default_headers: Additional HTTP headers
        temperature: Sampling temperature
        max_tokens: Maximum tokens to generate
        top_p: Top-p sampling parameter
        top_k: Top-k sampling parameter
        frequency_penalty: Frequency penalty
        presence_penalty: Presence penalty
        timeout: Request timeout in seconds
    """

    type: Literal["sap_ai_core_sdk"]
    model: str
    base_url: str
    resource_group: str
    deployment_id: str
    api_key: Optional[str] = Field(default=None, exclude=True)
    auth_url: Optional[str] = None
    client_id: Optional[str] = None
    client_secret: Optional[str] = Field(default=None, exclude=True)
    default_headers: Optional[dict[str, str]] = None
    temperature: Optional[float] = None
    max_tokens: Optional[int] = None
    top_p: Optional[float] = None
    top_k: Optional[int] = None
    frequency_penalty: Optional[float] = None
    presence_penalty: Optional[float] = None
    timeout: Optional[int] = 60

    @classmethod
    def supported_models(cls) -> list[str]:
        """Returns a list of supported models in regex for LlmRegistry."""
        # SAP AI Core supports various models through deployments
        return [r".*"]

    def __init__(self, **data):
        if not SAP_AI_SDK_AVAILABLE:
            raise ImportError(
                "SAP AI SDK is not available. Please install it with: "
                "pip install 'sap-ai-sdk-gen[all]'"
            )
        super().__init__(**data)

    @cached_property
    def _client(self) -> AICoreClient:
        """Get the SAP AI Core client using official SDK."""
        # Get API key from parameter or environment
        api_key = self.api_key or os.environ.get("SAP_AI_CORE_API_KEY")
        
        if not api_key:
            raise ValueError(
                "API key must be provided either via api_key parameter or "
                "SAP_AI_CORE_API_KEY environment variable"
            )
        
        # Configure client
        client_config = {
            "base_url": self.base_url,
            "api_key": api_key,
            "resource_group": self.resource_group,
            "timeout": self.timeout,
        }
        
        # Add OAuth configuration if provided
        if self.auth_url and self.client_id:
            client_secret = self.client_secret or os.environ.get("SAP_AI_CORE_CLIENT_SECRET")
            if client_secret:
                client_config.update({
                    "auth_url": self.auth_url,
                    "client_id": self.client_id,
                    "client_secret": client_secret,
                })
        
        return AICoreClient(**client_config)

    def _convert_content_to_messages(
        self, contents: list[types.Content], system_instruction: Optional[str] = None
    ) -> list[ChatMessage]:
        """Convert google.genai Content list to SAP AI Core SDK messages format."""
        messages = []
        
        # Add system message if provided
        if system_instruction:
            messages.append(ChatMessage(role="system", content=system_instruction))
        
        for content in contents:
            role = "assistant" if content.role == "model" else content.role
            
            # Extract text from parts
            text_parts = []
            for part in content.parts or []:
                if part.text:
                    text_parts.append(part.text)
            
            if text_parts:
                messages.append(ChatMessage(
                    role=role,
                    content="\n".join(text_parts)
                ))
        
        return messages

    def _convert_response_to_llm_response(self, response) -> LlmResponse:
        """Convert SAP AI Core SDK response to LlmResponse."""
        try:
            # Extract content from response
            content_text = ""
            if hasattr(response, 'choices') and response.choices:
                choice = response.choices[0]
                if hasattr(choice, 'message') and choice.message:
                    content_text = choice.message.content or ""
            
            # Create content
            parts = [types.Part.from_text(text=content_text)]
            content = types.Content(role="model", parts=parts)
            
            # Handle usage metadata
            usage_metadata = None
            if hasattr(response, 'usage') and response.usage:
                usage_metadata = types.GenerateContentResponseUsageMetadata(
                    prompt_token_count=response.usage.prompt_tokens or 0,
                    candidates_token_count=response.usage.completion_tokens or 0,
                    total_token_count=response.usage.total_tokens or 0,
                )
            
            # Handle finish reason
            finish_reason = types.FinishReason.STOP
            if hasattr(response, 'choices') and response.choices:
                choice = response.choices[0]
                if hasattr(choice, 'finish_reason'):
                    if choice.finish_reason == "length":
                        finish_reason = types.FinishReason.MAX_TOKENS
                    elif choice.finish_reason == "content_filter":
                        finish_reason = types.FinishReason.SAFETY
            
            return LlmResponse(
                content=content,
                usage_metadata=usage_metadata,
                finish_reason=finish_reason
            )
        except Exception as e:
            return LlmResponse(
                error_code="CONVERSION_ERROR",
                error_message=f"Failed to convert response: {str(e)}"
            )

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        """Generate content using SAP AI Core SDK.
        
        Args:
            llm_request: The LLM request containing messages and configuration
            stream: Whether to stream the response (currently not supported by SDK)
            
        Yields:
            LlmResponse objects containing the generated content
        """
        try:
            # Convert messages
            system_instruction = None
            if llm_request.config and llm_request.config.system_instruction:
                if isinstance(llm_request.config.system_instruction, str):
                    system_instruction = llm_request.config.system_instruction
                elif hasattr(llm_request.config.system_instruction, "parts"):
                    parts = getattr(llm_request.config.system_instruction, "parts", [])
                    text_parts = []
                    for part in parts:
                        if hasattr(part, "text") and part.text:
                            text_parts.append(part.text)
                    system_instruction = "\n".join(text_parts)
            
            messages = self._convert_content_to_messages(llm_request.contents, system_instruction)
            
            # Build request using SDK
            request = ChatCompletionRequest(
                model=llm_request.model or self.model,
                messages=messages,
                deployment_id=self.deployment_id,
            )
            
            # Add optional parameters
            if self.temperature is not None:
                request.temperature = self.temperature
            if self.max_tokens is not None:
                request.max_tokens = self.max_tokens
            if self.top_p is not None:
                request.top_p = self.top_p
            if self.top_k is not None:
                request.top_k = self.top_k
            if self.frequency_penalty is not None:
                request.frequency_penalty = self.frequency_penalty
            if self.presence_penalty is not None:
                request.presence_penalty = self.presence_penalty
            
            if stream:
                # Streaming support (if SDK supports it)
                async for chunk in self._client.chat_completions.create_stream(request):
                    if hasattr(chunk, 'choices') and chunk.choices:
                        choice = chunk.choices[0]
                        if hasattr(choice, 'delta') and choice.delta:
                            delta = choice.delta
                            if hasattr(delta, 'content') and delta.content:
                                content = types.Content(
                                    role="model",
                                    parts=[types.Part.from_text(text=delta.content)]
                                )
                                yield LlmResponse(
                                    content=content,
                                    partial=True,
                                    turn_complete=hasattr(choice, 'finish_reason') and choice.finish_reason is not None
                                )
            else:
                # Non-streaming request
                response = await self._client.chat_completions.create(request)
                yield self._convert_response_to_llm_response(response)
        
        except AICoreException as e:
            yield LlmResponse(
                error_code="SAP_AI_CORE_ERROR",
                error_message=f"SAP AI Core error: {str(e)}"
            )
        except Exception as e:
            yield LlmResponse(
                error_code="API_ERROR",
                error_message=f"Unexpected error: {str(e)}"
            )
