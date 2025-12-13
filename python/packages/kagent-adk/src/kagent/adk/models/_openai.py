from __future__ import annotations

import base64
import json
import os
from functools import cached_property
from typing import TYPE_CHECKING, Any, AsyncGenerator, Iterable, Literal, Optional

import httpx
from google.adk.models import BaseLlm
from google.adk.models.llm_response import LlmResponse
from google.genai import types
from google.genai.types import FunctionCall, FunctionResponse
from openai import AsyncAzureOpenAI, AsyncOpenAI, DefaultAsyncHttpxClient
from openai.types.chat import (
    ChatCompletion,
    ChatCompletionAssistantMessageParam,
    ChatCompletionContentPartImageParam,
    ChatCompletionContentPartTextParam,
    ChatCompletionMessageParam,
    ChatCompletionSystemMessageParam,
    ChatCompletionToolMessageParam,
    ChatCompletionToolParam,
    ChatCompletionUserMessageParam,
)
from openai.types.chat.chat_completion_message_tool_call_param import (
    ChatCompletionMessageToolCallParam,
)
from openai.types.chat.chat_completion_message_tool_call_param import (
    Function as ToolCallFunction,
)
from openai.types.shared_params import FunctionDefinition, FunctionParameters
from pydantic import Field

from ._ssl import create_ssl_context

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest


def _convert_role_to_openai(role: Optional[str]) -> str:
    """Convert google.genai role to OpenAI role."""
    if role in ["model", "assistant"]:
        return "assistant"
    elif role == "system":
        return "system"
    else:
        return "user"


def _convert_content_to_openai_messages(
    contents: list[types.Content], system_instruction: Optional[str] = None
) -> list[ChatCompletionMessageParam]:
    """Convert google.genai Content list to OpenAI messages format."""
    messages: list[ChatCompletionMessageParam] = []

    # Add system message if provided
    if system_instruction:
        system_message: ChatCompletionSystemMessageParam = {"role": "system", "content": system_instruction}
        messages.append(system_message)

    # First pass: collect all function responses to match with tool calls
    all_function_responses: dict[str, FunctionResponse] = {}
    for content in contents:
        for part in content.parts or []:
            if part.function_response:
                tool_call_id = part.function_response.id or "call_1"
                all_function_responses[tool_call_id] = part.function_response

    for content in contents:
        role = _convert_role_to_openai(content.role)

        # Separate different types of parts
        text_parts: list[str] = []
        function_calls: list[FunctionCall] = []
        function_responses: list[FunctionResponse] = []
        image_parts = []

        for part in content.parts or []:
            if part.text:
                text_parts.append(part.text)
            elif part.function_call:
                function_calls.append(part.function_call)
            elif part.function_response:
                function_responses.append(part.function_response)
            elif part.inline_data and part.inline_data.mime_type and part.inline_data.mime_type.startswith("image"):
                if part.inline_data.data:
                    image_data = base64.b64encode(part.inline_data.data).decode()
                    image_part: ChatCompletionContentPartImageParam = {
                        "type": "image_url",
                        "image_url": {"url": f"data:{part.inline_data.mime_type};base64,{image_data}"},
                    }
                    image_parts.append(image_part)

        # Function responses are now handled together with function calls
        # This ensures proper pairing and prevents orphaned tool messages

        # Handle function calls (assistant messages with tool_calls)
        if function_calls:
            tool_calls = []
            tool_response_messages = []

            for func_call in function_calls:
                tool_call_function: ToolCallFunction = {
                    "name": func_call.name or "",
                    "arguments": json.dumps(func_call.args) if func_call.args else "{}",
                }
                tool_call_id = func_call.id or "call_1"
                tool_call = ChatCompletionMessageToolCallParam(
                    id=tool_call_id,
                    type="function",
                    function=tool_call_function,
                )
                tool_calls.append(tool_call)

                # Check if we have a response for this tool call
                if tool_call_id in all_function_responses:
                    func_response = all_function_responses[tool_call_id]
                    content = ""
                    if isinstance(func_response.response, str):
                        content = func_response.response
                    elif func_response.response and "content" in func_response.response:
                        content_list = func_response.response["content"]
                        if len(content_list) > 0:
                            content = content_list[0]["text"]
                    elif func_response.response and "result" in func_response.response:
                        content = func_response.response["result"]

                    tool_message = ChatCompletionToolMessageParam(
                        role="tool",
                        tool_call_id=tool_call_id,
                        content=content,
                    )
                    tool_response_messages.append(tool_message)
                else:
                    # If no response is available, create a placeholder response
                    # This prevents the OpenAI API error
                    tool_message = ChatCompletionToolMessageParam(
                        role="tool",
                        tool_call_id=tool_call_id,
                        content="No response available for this function call.",
                    )
                    tool_response_messages.append(tool_message)

            # Create assistant message with tool calls
            text_content = "\n".join(text_parts) if text_parts else None
            assistant_message = ChatCompletionAssistantMessageParam(
                role="assistant",
                content=text_content,
                tool_calls=tool_calls,
            )
            messages.append(assistant_message)

            # Add all tool response messages immediately after the assistant message
            messages.extend(tool_response_messages)

        # Handle regular text/image messages (only if no function calls)
        elif text_parts or image_parts:
            if role == "user":
                if image_parts and text_parts:
                    # Multi-modal content
                    text_part = ChatCompletionContentPartTextParam(type="text", text="\n".join(text_parts))
                    content_parts = [text_part] + image_parts
                    user_message = ChatCompletionUserMessageParam(role="user", content=content_parts)
                elif image_parts:
                    # Image only
                    user_message = ChatCompletionUserMessageParam(role="user", content=image_parts)
                else:
                    # Text only
                    user_message = ChatCompletionUserMessageParam(role="user", content="\n".join(text_parts))
                messages.append(user_message)
            elif role == "assistant":
                # Assistant messages with text (no tool calls)
                assistant_message = ChatCompletionAssistantMessageParam(
                    role="assistant",
                    content="\n".join(text_parts),
                )
                messages.append(assistant_message)

    return messages


def _update_type_string(value_dict: dict[str, Any]):
    """Updates 'type' field to expected JSON schema format."""
    if "type" in value_dict:
        value_dict["type"] = value_dict["type"].lower()

    if "items" in value_dict:
        # 'type' field could exist for items as well, this would be the case if
        # items represent primitive types.
        _update_type_string(value_dict["items"])

        if "properties" in value_dict["items"]:
            # There could be properties as well on the items, especially if the items
            # are complex object themselves. We recursively traverse each individual
            # property as well and fix the "type" value.
            for _, value in value_dict["items"]["properties"].items():
                _update_type_string(value)

    if "properties" in value_dict:
        # Handle nested properties
        for _, value in value_dict["properties"].items():
            _update_type_string(value)


def _convert_tools_to_openai(tools: list[types.Tool]) -> list[ChatCompletionToolParam]:
    """Convert google.genai Tools to OpenAI tools format."""
    openai_tools: list[ChatCompletionToolParam] = []

    for tool in tools:
        if tool.function_declarations:
            for func_decl in tool.function_declarations:
                # Build function definition
                function_def = FunctionDefinition(
                    name=func_decl.name or "",
                    description=func_decl.description or "",
                )

                # Always include parameters field, even if empty
                properties = {}
                required = []

                if func_decl.parameters:
                    if func_decl.parameters.properties:
                        for prop_name, prop_schema in func_decl.parameters.properties.items():
                            value_dict = prop_schema.model_dump(exclude_none=True)
                            _update_type_string(value_dict)
                            properties[prop_name] = value_dict

                    if func_decl.parameters.required:
                        required = func_decl.parameters.required

                function_def["parameters"] = {"type": "object", "properties": properties, "required": required}

                # Create the tool param
                openai_tool = ChatCompletionToolParam(type="function", function=function_def)
                openai_tools.append(openai_tool)

    return openai_tools


def _convert_openai_response_to_llm_response(response: ChatCompletion) -> LlmResponse:
    """Convert OpenAI response to LlmResponse."""
    choice = response.choices[0]
    message = choice.message

    parts = []

    # Handle text content
    if message.content:
        parts.append(types.Part.from_text(text=message.content))

    # Handle function calls
    if hasattr(message, "tool_calls") and message.tool_calls:
        for tool_call in message.tool_calls:
            if tool_call.type == "function":
                try:
                    args = json.loads(tool_call.function.arguments) if tool_call.function.arguments else {}
                except json.JSONDecodeError:
                    args = {}

                part = types.Part.from_function_call(name=tool_call.function.name, args=args)
                if part.function_call:
                    part.function_call.id = tool_call.id
                parts.append(part)

    content = types.Content(role="model", parts=parts)

    # Handle usage metadata
    usage_metadata = None
    if hasattr(response, "usage") and response.usage:
        usage_metadata = types.GenerateContentResponseUsageMetadata(
            prompt_token_count=response.usage.prompt_tokens,
            candidates_token_count=response.usage.completion_tokens,
            total_token_count=response.usage.total_tokens,
        )

    # Handle finish reason
    finish_reason = types.FinishReason.STOP
    if choice.finish_reason == "length":
        finish_reason = types.FinishReason.MAX_TOKENS
    elif choice.finish_reason == "content_filter":
        finish_reason = types.FinishReason.SAFETY

    return LlmResponse(content=content, usage_metadata=usage_metadata, finish_reason=finish_reason)


class BaseOpenAI(BaseLlm):
    """Base class for OpenAI-compatible models."""

    model: str
    api_key: Optional[str] = Field(default=None, exclude=True)
    base_url: Optional[str] = None
    frequency_penalty: Optional[float] = None
    default_headers: Optional[dict[str, str]] = None
    max_tokens: Optional[int] = None
    n: Optional[int] = None
    presence_penalty: Optional[float] = None
    reasoning_effort: Optional[str] = None
    seed: Optional[int] = None
    temperature: Optional[float] = None
    timeout: Optional[int] = None
    top_p: Optional[float] = None

    # TLS/SSL configuration fields
    tls_disable_verify: Optional[bool] = None
    tls_ca_cert_path: Optional[str] = None
    tls_disable_system_cas: Optional[bool] = None

    @classmethod
    def supported_models(cls) -> list[str]:
        """Returns a list of supported models in regex for LlmRegistry."""
        return [r"gpt-.*", r"o1-.*"]

    def _get_tls_config(self) -> tuple[bool, Optional[str], bool]:
        """Read TLS configuration from instance fields.

        Returns:
            Tuple of (disable_verify, ca_cert_path, disable_system_cas)
        """
        # Read from instance fields only (config-based approach)
        # Environment variables are no longer supported for TLS configuration
        disable_verify = self.tls_disable_verify or False
        ca_cert_path = self.tls_ca_cert_path
        disable_system_cas = self.tls_disable_system_cas or False

        return disable_verify, ca_cert_path, disable_system_cas

    def _create_http_client(self) -> Optional[httpx.AsyncClient]:
        """Create HTTP client with custom SSL context using OpenAI SDK defaults.

        Uses DefaultAsyncHttpxClient to preserve OpenAI's default settings for
        timeout, connection pooling, and redirect behavior while applying custom
        SSL configuration.

        Returns:
            DefaultAsyncHttpxClient with SSL configuration, or None if no TLS config
        """
        disable_verify, ca_cert_path, disable_system_cas = self._get_tls_config()

        # Only create custom http client if TLS configuration is present
        if disable_verify or ca_cert_path or disable_system_cas:
            ssl_context = create_ssl_context(
                disable_verify=disable_verify,
                ca_cert_path=ca_cert_path,
                disable_system_cas=disable_system_cas,
            )

            # ssl_context is either False (verification disabled) or SSLContext
            # Use DefaultAsyncHttpxClient to preserve OpenAI's defaults
            return DefaultAsyncHttpxClient(verify=ssl_context)

        # No TLS configuration, return None to use OpenAI SDK default
        return None

    @cached_property
    def _client(self) -> AsyncOpenAI:
        """Get the OpenAI client with optional custom SSL configuration."""
        http_client = self._create_http_client()

        return AsyncOpenAI(
            api_key=self.api_key,
            base_url=self.base_url or None,
            default_headers=self.default_headers,
            timeout=self.timeout,
            http_client=http_client,
        )

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        """Generate content using OpenAI API."""

        # Convert messages
        system_instruction = None
        if llm_request.config and llm_request.config.system_instruction:
            if isinstance(llm_request.config.system_instruction, str):
                system_instruction = llm_request.config.system_instruction
            elif hasattr(llm_request.config.system_instruction, "parts"):
                # Handle Content type system instruction
                text_parts = []
                parts = getattr(llm_request.config.system_instruction, "parts", [])
                if parts:
                    for part in parts:
                        if hasattr(part, "text") and part.text:
                            text_parts.append(part.text)
                    system_instruction = "\n".join(text_parts)

        messages = _convert_content_to_openai_messages(llm_request.contents, system_instruction)

        # Prepare request parameters
        kwargs = {
            "model": llm_request.model or self.model,
            "messages": messages,
        }

        if self.frequency_penalty is not None:
            kwargs["frequency_penalty"] = self.frequency_penalty
        if self.max_tokens:
            kwargs["max_tokens"] = self.max_tokens
        if self.n is not None:
            kwargs["n"] = self.n
        if self.presence_penalty is not None:
            kwargs["presence_penalty"] = self.presence_penalty
        if self.reasoning_effort is not None:
            kwargs["reasoning_effort"] = self.reasoning_effort
        if self.seed is not None:
            kwargs["seed"] = self.seed
        if self.temperature is not None:
            kwargs["temperature"] = self.temperature
        if self.top_p is not None:
            kwargs["top_p"] = self.top_p

        # Handle tools
        if llm_request.config and llm_request.config.tools:
            # Filter to only google.genai.types.Tool objects
            genai_tools = []
            for tool in llm_request.config.tools:
                if hasattr(tool, "function_declarations"):
                    genai_tools.append(tool)

            if genai_tools:
                openai_tools = _convert_tools_to_openai(genai_tools)
                if openai_tools:
                    kwargs["tools"] = openai_tools
                    kwargs["tool_choice"] = "auto"

        try:
            if stream:
                # Handle streaming
                async for chunk in await self._client.chat.completions.create(stream=True, **kwargs):
                    if chunk.choices and chunk.choices[0].delta:
                        delta = chunk.choices[0].delta
                        if delta.content:
                            content = types.Content(role="model", parts=[types.Part.from_text(text=delta.content)])
                            yield LlmResponse(
                                content=content, partial=True, turn_complete=chunk.choices[0].finish_reason is not None
                            )
            else:
                # Handle non-streaming
                response = await self._client.chat.completions.create(stream=False, **kwargs)
                yield _convert_openai_response_to_llm_response(response)

        except Exception as e:
            yield LlmResponse(error_code="API_ERROR", error_message=str(e))


class OpenAI(BaseOpenAI):
    """OpenAI model implementation."""

    type: Literal["openai"]


class AzureOpenAI(BaseOpenAI):
    """Azure OpenAI model implementation."""

    type: Literal["azure_openai"]
    api_version: Optional[str] = None
    azure_endpoint: Optional[str] = None
    azure_deployment: Optional[str] = None

    @cached_property
    def _client(self) -> AsyncAzureOpenAI:
        """Get the Azure OpenAI client with optional custom SSL configuration."""
        api_version = self.api_version or os.environ.get("OPENAI_API_VERSION", "2024-02-15-preview")
        azure_endpoint = self.azure_endpoint or os.environ.get("AZURE_OPENAI_ENDPOINT")
        api_key = self.api_key or os.environ.get("AZURE_OPENAI_API_KEY")

        if not azure_endpoint:
            raise ValueError(
                "Azure endpoint must be provided either via azure_endpoint parameter or AZURE_OPENAI_ENDPOINT environment variable"
            )

        if not api_key:
            raise ValueError(
                "API key must be provided either via api_key parameter or AZURE_OPENAI_API_KEY environment variable"
            )

        http_client = self._create_http_client()

        return AsyncAzureOpenAI(
            api_key=api_key,
            api_version=api_version,
            azure_endpoint=azure_endpoint,
            default_headers=self.default_headers,
            http_client=http_client,
        )
