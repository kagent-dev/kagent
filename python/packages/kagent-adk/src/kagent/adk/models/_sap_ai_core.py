"""SAP AI Core model implementation for KAgent."""

from __future__ import annotations

import json
import os
from functools import cached_property
from typing import TYPE_CHECKING, AsyncGenerator, Literal, Optional

import httpx
from google.adk.models import BaseLlm
from google.adk.models.llm_response import LlmResponse
from google.genai import types
from pydantic import Field

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest


class SAPAICore(BaseLlm):
    """SAP AI Core model implementation.
    
    This adapter enables KAgent to call SAP AI Core generative AI deployments.
    It supports OAuth2 authentication and various LLM parameters.
    
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

    type: Literal["sap_ai_core"]
    model: str
    base_url: str
    resource_group: str
    deployment_id: str
    api_key: Optional[str] = Field(default=None, exclude=True)
    auth_url: Optional[str] = None
    client_id: Optional[str] = None
    client_secret: Optional[str] = Field(default=None, exclude=True)
    default_headers: Optional[dict[str, str]] = None
    temperature: Optional[str] = None
    max_tokens: Optional[int] = None
    top_p: Optional[str] = None
    top_k: Optional[int] = None
    frequency_penalty: Optional[str] = None
    presence_penalty: Optional[str] = None
    timeout: Optional[int] = 60

    @classmethod
    def supported_models(cls) -> list[str]:
        """Returns a list of supported models in regex for LlmRegistry."""
        # SAP AI Core supports various models through deployments
        return [r".*"]

    @cached_property
    def _client(self) -> httpx.AsyncClient:
        """Get the HTTP client with authentication."""
        headers = self.default_headers.copy() if self.default_headers else {}
        
        # Get API key from parameter or environment
        api_key = self.api_key or os.environ.get("SAP_AI_CORE_API_KEY")
        
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"
        
        # Add SAP AI Core specific headers
        headers["AI-Resource-Group"] = self.resource_group
        headers["Content-Type"] = "application/json"
        
        return httpx.AsyncClient(
            base_url=self.base_url,
            headers=headers,
            timeout=self.timeout,
        )

    async def _get_oauth_token(self) -> Optional[str]:
        """Get OAuth2 access token if OAuth is configured."""
        if not all([self.auth_url, self.client_id]):
            return None
        
        client_secret = self.client_secret or os.environ.get("SAP_AI_CORE_CLIENT_SECRET")
        if not client_secret:
            return None
        
        try:
            async with httpx.AsyncClient() as client:
                response = await client.post(
                    self.auth_url,
                    data={
                        "grant_type": "client_credentials",
                        "client_id": self.client_id,
                        "client_secret": client_secret,
                    },
                    headers={"Content-Type": "application/x-www-form-urlencoded"},
                )
                response.raise_for_status()
                token_data = response.json()
                return token_data.get("access_token")
        except Exception as e:
            print(f"Failed to get OAuth token: {e}")
            return None

    def _convert_content_to_messages(
        self, contents: list[types.Content], system_instruction: Optional[str] = None
    ) -> list[dict]:
        """Convert google.genai Content list to SAP AI Core messages format."""
        messages = []
        
        # Add system message if provided
        if system_instruction:
            messages.append({"role": "system", "content": system_instruction})
        
        for content in contents:
            role = "assistant" if content.role == "model" else content.role
            
            # Extract text from parts
            text_parts = []
            for part in content.parts or []:
                if part.text:
                    text_parts.append(part.text)
            
            if text_parts:
                messages.append({
                    "role": role,
                    "content": "\n".join(text_parts)
                })
        
        return messages

    def _convert_response_to_llm_response(self, response_data: dict) -> LlmResponse:
        """Convert SAP AI Core response to LlmResponse."""
        # SAP AI Core typically returns OpenAI-compatible format
        choices = response_data.get("choices", [])
        if not choices:
            return LlmResponse(error_code="API_ERROR", error_message="No choices in response")
        
        choice = choices[0]
        message = choice.get("message", {})
        content_text = message.get("content", "")
        
        # Create content
        parts = [types.Part.from_text(text=content_text)]
        content = types.Content(role="model", parts=parts)
        
        # Handle usage metadata
        usage_metadata = None
        if "usage" in response_data:
            usage = response_data["usage"]
            usage_metadata = types.GenerateContentResponseUsageMetadata(
                prompt_token_count=usage.get("prompt_tokens", 0),
                candidates_token_count=usage.get("completion_tokens", 0),
                total_token_count=usage.get("total_tokens", 0),
            )
        
        # Handle finish reason
        finish_reason = types.FinishReason.STOP
        if choice.get("finish_reason") == "length":
            finish_reason = types.FinishReason.MAX_TOKENS
        
        return LlmResponse(
            content=content,
            usage_metadata=usage_metadata,
            finish_reason=finish_reason
        )

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        """Generate content using SAP AI Core API.
        
        Args:
            llm_request: The LLM request containing messages and configuration
            stream: Whether to stream the response (currently not supported)
            
        Yields:
            LlmResponse objects containing the generated content
        """
        # Get OAuth token if configured
        oauth_token = await self._get_oauth_token()
        
        # Build request headers
        headers = {}
        if oauth_token:
            headers["Authorization"] = f"Bearer {oauth_token}"
        
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
        
        # Build request payload
        payload = {
            "messages": messages,
            "model": llm_request.model or self.model,
        }
        
        # Add optional parameters with string to float conversion
        if self.temperature is not None:
            try:
                payload["temperature"] = float(self.temperature)
            except (ValueError, TypeError):
                logger.warning(f"Invalid temperature value: {self.temperature}")
        if self.max_tokens is not None:
            payload["max_tokens"] = self.max_tokens
        if self.top_p is not None:
            try:
                payload["top_p"] = float(self.top_p)
            except (ValueError, TypeError):
                logger.warning(f"Invalid top_p value: {self.top_p}")
        if self.frequency_penalty is not None:
            try:
                payload["frequency_penalty"] = float(self.frequency_penalty)
            except (ValueError, TypeError):
                logger.warning(f"Invalid frequency_penalty value: {self.frequency_penalty}")
        if self.presence_penalty is not None:
            try:
                payload["presence_penalty"] = float(self.presence_penalty)
            except (ValueError, TypeError):
                logger.warning(f"Invalid presence_penalty value: {self.presence_penalty}")
        
        # SAP AI Core inference endpoint
        endpoint = f"/v2/inference/deployments/{self.deployment_id}/chat/completions"
        
        try:
            if stream:
                # Streaming support (if SAP AI Core supports it)
                payload["stream"] = True
                async with self._client.stream("POST", endpoint, json=payload, headers=headers) as response:
                    response.raise_for_status()
                    async for line in response.aiter_lines():
                        if line.startswith("data: "):
                            data = line[6:]
                            if data.strip() == "[DONE]":
                                break
                            try:
                                chunk = json.loads(data)
                                choices = chunk.get("choices", [])
                                if choices and choices[0].get("delta", {}).get("content"):
                                    content_text = choices[0]["delta"]["content"]
                                    content = types.Content(
                                        role="model",
                                        parts=[types.Part.from_text(text=content_text)]
                                    )
                                    yield LlmResponse(
                                        content=content,
                                        partial=True,
                                        turn_complete=choices[0].get("finish_reason") is not None
                                    )
                            except json.JSONDecodeError:
                                continue
            else:
                # Non-streaming request
                response = await self._client.post(endpoint, json=payload, headers=headers)
                response.raise_for_status()
                response_data = response.json()
                yield self._convert_response_to_llm_response(response_data)
        
        except httpx.HTTPStatusError as e:
            error_msg = f"HTTP {e.response.status_code}: {e.response.text}"
            yield LlmResponse(error_code="HTTP_ERROR", error_message=error_msg)
        except Exception as e:
            yield LlmResponse(error_code="API_ERROR", error_message=str(e))

