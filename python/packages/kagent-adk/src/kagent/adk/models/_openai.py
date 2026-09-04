from __future__ import annotations

import base64
import json
import logging
import os
from functools import cached_property
from typing import TYPE_CHECKING, Any, AsyncGenerator, Iterable, Literal, Optional, cast

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
from openai.types.responses import (
    EasyInputMessageParam,
    FunctionToolParam,
    Response,
    ResponseFunctionToolCall,
    ResponseFunctionToolCallParam,
    ResponseInputImageParam,
    ResponseInputItemParam,
    ResponseInputTextParam,
    ResponseOutputMessage,
    ResponseUsage,
)
from openai.types.responses.response_input_item_param import FunctionCallOutput
from openai.types.responses.response_input_message_content_list_param import (
    ResponseInputMessageContentListParam,
)
from openai.types.shared_params import FunctionDefinition, FunctionParameters
from pydantic import Field, model_validator

from ._ssl import KAgentTLSMixin
from ._token_source import GDCHTokenSource
from ._utils import function_declaration_schema

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

logger = logging.getLogger(__name__)

# OpenAI API format (ModelConfig openAI.apiFormat); chatCompletions is the default.
OpenAIAPIFormat = Literal["chatCompletions", "responses"]
OPENAI_API_FORMAT_RESPONSES = "responses"

# Sampling parameters the Responses API does not accept.
_RESPONSES_UNSUPPORTED_PARAMS = ("frequency_penalty", "presence_penalty", "n", "seed")


def _convert_role_to_openai(role: Optional[str]) -> str:
    """Convert google.genai role to OpenAI role."""
    if role in ["model", "assistant"]:
        return "assistant"
    elif role == "system":
        return "system"
    else:
        return "user"


def _extract_thought_signature(extra_content: Any) -> Optional[str]:
    """Extract a Gemini thought signature from OpenAI-compatible extra content."""
    if not isinstance(extra_content, dict):
        return None

    google_extra = extra_content.get("google")
    if not isinstance(google_extra, dict):
        return None

    thought_signature = google_extra.get("thought_signature")
    if isinstance(thought_signature, str) and thought_signature:
        return thought_signature

    return None


def _openai_extra_content_for_thought_signature(thought_signature: Optional[bytes]) -> Optional[dict[str, Any]]:
    """Convert a Part thought signature into OpenAI-compatible extra content."""
    if not thought_signature:
        return None

    return {
        "google": {
            "thought_signature": base64.b64encode(thought_signature).decode("utf-8"),
        }
    }


def _thought_signatures_by_tool_call_id(contents: list[types.Content]) -> dict[str, bytes]:
    """Index function call thought signatures by tool call id."""
    thought_signatures: dict[str, bytes] = {}
    for content in contents:
        for part in content.parts or []:
            if part.function_call and part.thought_signature:
                tool_call_id = part.function_call.id or "call_1"
                thought_signatures[tool_call_id] = part.thought_signature

    return thought_signatures


def _function_responses_by_tool_call_id(contents: list[types.Content]) -> dict[str, FunctionResponse]:
    """Index function responses by tool call id."""
    function_responses: dict[str, FunctionResponse] = {}
    for content in contents:
        for part in content.parts or []:
            if part.function_response:
                function_responses[part.function_response.id or "call_1"] = part.function_response

    return function_responses


def _partition_content_parts(
    content: types.Content,
) -> tuple[list[str], list[FunctionCall], list[types.Blob]]:
    """Split a Content's parts into text, function calls and inline image blobs."""
    text_parts: list[str] = []
    function_calls: list[FunctionCall] = []
    image_blobs: list[types.Blob] = []

    for part in content.parts or []:
        if part.text:
            text_parts.append(part.text)
        elif part.function_call:
            function_calls.append(part.function_call)
        elif (
            part.inline_data
            and part.inline_data.data
            and part.inline_data.mime_type
            and part.inline_data.mime_type.startswith("image")
        ):
            image_blobs.append(part.inline_data)

    return text_parts, function_calls, image_blobs


def _build_function_call_part(
    *,
    name: str,
    args: dict[str, Any],
    tool_call_id: str,
    thought_signature: Optional[str] = None,
) -> types.Part:
    """Build a function-call part, preserving thought signatures when present."""
    if thought_signature:
        return types.Part.model_validate(
            {
                "functionCall": {
                    "id": tool_call_id,
                    "name": name,
                    "args": args,
                },
                "thoughtSignature": thought_signature,
            },
            by_alias=True,
        )

    part = types.Part.from_function_call(name=name, args=args)
    if part.function_call:
        part.function_call.id = tool_call_id
    return part


def _blob_data_uri(blob: types.Blob) -> str:
    """Render an inline data blob as a base64 data URI."""
    return f"data:{blob.mime_type};base64,{base64.b64encode(blob.data or b'').decode()}"


def _extract_function_response_content(func_response: FunctionResponse) -> str:
    """Extract text content from a genai FunctionResponse for the model to consume."""
    if isinstance(func_response.response, str):
        return func_response.response
    if func_response.response and "content" in func_response.response:
        content_list = func_response.response["content"]
        if len(content_list) > 0:
            return "\n".join(item["text"] for item in content_list if "text" in item)
    elif func_response.response and "result" in func_response.response:
        return str(func_response.response["result"])
    return ""


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
    all_function_responses = _function_responses_by_tool_call_id(contents)
    thought_signatures = _thought_signatures_by_tool_call_id(contents)

    for content in contents:
        role = _convert_role_to_openai(content.role)

        text_parts, function_calls, image_blobs = _partition_content_parts(content)
        image_parts: list[ChatCompletionContentPartImageParam] = [
            {"type": "image_url", "image_url": {"url": _blob_data_uri(blob)}} for blob in image_blobs
        ]

        # Function responses are handled together with function calls
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
                if extra_content := _openai_extra_content_for_thought_signature(thought_signatures.get(tool_call_id)):
                    tool_call["extra_content"] = extra_content
                tool_calls.append(tool_call)

                # Check if we have a response for this tool call
                if tool_call_id in all_function_responses:
                    tool_message = ChatCompletionToolMessageParam(
                        role="tool",
                        tool_call_id=tool_call_id,
                        content=_extract_function_response_content(all_function_responses[tool_call_id]),
                    )
                    if extra_content := _openai_extra_content_for_thought_signature(
                        thought_signatures.get(tool_call_id)
                    ):
                        tool_message["extra_content"] = extra_content
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


def _iter_function_declarations(tools: list[types.Tool]) -> Iterable[types.FunctionDeclaration]:
    """Yield every function declaration across a list of genai Tools."""
    for tool in tools:
        for func_decl in tool.function_declarations or []:
            yield func_decl


def _convert_tools_to_openai(tools: list[types.Tool]) -> list[ChatCompletionToolParam]:
    """Convert google.genai Tools to OpenAI tools format."""
    openai_tools: list[ChatCompletionToolParam] = []

    for func_decl in _iter_function_declarations(tools):
        function_def = FunctionDefinition(
            name=func_decl.name or "",
            description=func_decl.description or "",
        )

        parameters = function_declaration_schema(func_decl)
        parameters.setdefault("required", [])
        function_def["parameters"] = cast(FunctionParameters, parameters)

        openai_tools.append(ChatCompletionToolParam(type="function", function=function_def))

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

                part = _build_function_call_part(
                    name=tool_call.function.name,
                    args=args,
                    tool_call_id=tool_call.id,
                    thought_signature=_extract_thought_signature(
                        getattr(tool_call, "model_extra", {}).get("extra_content")
                    ),
                )
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


def _extract_system_instruction(llm_request: LlmRequest) -> Optional[str]:
    """Extract the system instruction text from an LlmRequest's config, if any."""
    if not (llm_request.config and llm_request.config.system_instruction):
        return None

    system_instruction = llm_request.config.system_instruction
    if isinstance(system_instruction, str):
        return system_instruction
    if hasattr(system_instruction, "parts"):
        text_parts = []
        parts = getattr(system_instruction, "parts", [])
        if parts:
            for part in parts:
                if hasattr(part, "text") and part.text:
                    text_parts.append(part.text)
            return "\n".join(text_parts)
    return None


def _convert_content_to_responses_input(contents: list[types.Content]) -> list[ResponseInputItemParam]:
    """Convert google.genai Content list to OpenAI Responses API input items."""
    input_items: list[ResponseInputItemParam] = []

    # First pass: collect all function responses to match with tool calls
    all_function_responses = _function_responses_by_tool_call_id(contents)

    for content in contents:
        role = _convert_role_to_openai(content.role)
        if role == "system":
            continue

        text_parts, function_calls, image_blobs = _partition_content_parts(content)
        image_parts = [
            ResponseInputImageParam(type="input_image", detail="auto", image_url=_blob_data_uri(blob))
            for blob in image_blobs
        ]

        # Handle function calls (assistant tool calls + their outputs)
        if function_calls and role == "assistant":
            if text_parts:
                input_items.append(EasyInputMessageParam(role="assistant", content="\n".join(text_parts)))

            for func_call in function_calls:
                tool_call_id = func_call.id or "call_1"
                input_items.append(
                    ResponseFunctionToolCallParam(
                        type="function_call",
                        call_id=tool_call_id,
                        name=func_call.name or "",
                        arguments=json.dumps(func_call.args) if func_call.args else "{}",
                    )
                )

                if tool_call_id in all_function_responses:
                    output = _extract_function_response_content(all_function_responses[tool_call_id])
                else:
                    # If no response is available, create a placeholder response
                    # This prevents the OpenAI API error
                    output = "No response available for this function call."
                input_items.append(FunctionCallOutput(type="function_call_output", call_id=tool_call_id, output=output))
            continue

        # Handle regular text/image messages (only if no function calls)
        if text_parts or image_parts:
            if image_parts:
                message_content: ResponseInputMessageContentListParam = [
                    ResponseInputTextParam(type="input_text", text=t) for t in text_parts
                ]
                message_content.extend(image_parts)
                input_items.append(EasyInputMessageParam(role=role, content=message_content))
            else:
                input_items.append(EasyInputMessageParam(role=role, content="\n".join(text_parts)))

    return input_items


def _convert_tools_to_responses(tools: list[types.Tool]) -> list[FunctionToolParam]:
    """Convert google.genai Tools to OpenAI Responses API tools format."""
    responses_tools: list[FunctionToolParam] = []

    for func_decl in _iter_function_declarations(tools):
        parameters = function_declaration_schema(func_decl)
        parameters.setdefault("required", [])
        responses_tools.append(
            FunctionToolParam(
                type="function",
                name=func_decl.name or "",
                description=func_decl.description or "",
                parameters=parameters,
                strict=False,
            )
        )

    return responses_tools


def _responses_usage_to_genai(
    usage: Optional[ResponseUsage],
) -> Optional[types.GenerateContentResponseUsageMetadata]:
    """Convert a Responses API usage block to genai usage metadata."""
    if usage is None:
        return None
    return types.GenerateContentResponseUsageMetadata(
        prompt_token_count=usage.input_tokens,
        candidates_token_count=usage.output_tokens,
        total_token_count=usage.total_tokens,
    )


def _responses_status_to_finish_reason(status: Optional[str]) -> types.FinishReason:
    """Map a Responses API response status to a genai finish reason."""
    if status == "incomplete":
        return types.FinishReason.MAX_TOKENS
    if status == "failed":
        return types.FinishReason.OTHER
    return types.FinishReason.STOP


def _responses_error_message(code: Optional[str], message: Optional[str]) -> str:
    """Render a Responses API error, keeping the upstream code when present."""
    message = message or "OpenAI responses request failed"
    return f"{code}: {message}" if code else message


def _responses_failure_llm_response(response: Optional[Response]) -> Optional[LlmResponse]:
    """Build an error LlmResponse for a failed Responses API response, else None."""
    if response is None or response.status != "failed":
        return None

    error = response.error
    return LlmResponse(
        error_code="API_ERROR",
        error_message=_responses_error_message(
            getattr(error, "code", None),
            getattr(error, "message", None),
        ),
    )


def _responses_output_message_texts(item: ResponseOutputMessage) -> list[str]:
    """Collect the text content of a Responses API output message."""
    return [text for output_content in item.content if (text := getattr(output_content, "text", None))]


def _responses_tool_call_part(item: ResponseFunctionToolCall) -> types.Part:
    """Build a genai function-call part from a Responses API tool call."""
    try:
        args = json.loads(item.arguments) if item.arguments else {}
    except json.JSONDecodeError:
        args = {}
    return _build_function_call_part(name=item.name, args=args, tool_call_id=item.call_id or item.id or "call_1")


def _convert_responses_output_to_llm_response(response: Response) -> LlmResponse:
    """Convert an OpenAI Responses API response to LlmResponse."""
    parts: list[types.Part] = []

    for item in response.output:
        if isinstance(item, ResponseOutputMessage):
            parts.extend(types.Part.from_text(text=text) for text in _responses_output_message_texts(item))
        elif isinstance(item, ResponseFunctionToolCall):
            parts.append(_responses_tool_call_part(item))

    content = types.Content(role="model", parts=parts)

    return LlmResponse(
        content=content,
        usage_metadata=_responses_usage_to_genai(response.usage),
        finish_reason=_responses_status_to_finish_reason(response.status),
    )


class BaseOpenAI(KAgentTLSMixin, BaseLlm):
    """Base class for OpenAI-compatible models."""

    model: str
    api_key: Optional[str] = Field(default=None, exclude=True)
    base_url: Optional[str] = None
    api_format: Optional[OpenAIAPIFormat] = None
    frequency_penalty: Optional[float] = None
    default_headers: Optional[dict[str, str]] = None
    max_tokens: Optional[int] = None
    max_completion_tokens: Optional[int] = None
    n: Optional[int] = None
    presence_penalty: Optional[float] = None
    reasoning_effort: Optional[str] = None
    seed: Optional[int] = None
    temperature: Optional[float] = None
    timeout: Optional[int] = None
    top_p: Optional[float] = None

    # API key passthrough: forward the Bearer token from incoming requests as the LLM API key
    api_key_passthrough: Optional[bool] = None

    # GDCH token exchange: refreshes a short-lived bearer token before each model call.
    token_exchange: Optional[GDCHTokenSource] = Field(default=None, exclude=True)

    @model_validator(mode="after")
    def _warn_on_ignored_responses_params(self) -> "BaseOpenAI":
        if self.api_format == OPENAI_API_FORMAT_RESPONSES:
            ignored = [name for name in _RESPONSES_UNSUPPORTED_PARAMS if getattr(self, name) is not None]
            if ignored:
                logger.warning(
                    "Ignoring %s for model %s: not supported by the OpenAI Responses API",
                    ", ".join(ignored),
                    self.model,
                )
        return self

    def set_passthrough_key(self, token: str) -> None:
        if self.api_key != token:
            self.api_key = token
            self.__dict__.pop("_client", None)  # invalidate cached client

    @classmethod
    def supported_models(cls) -> list[str]:
        """Returns a list of supported models in regex for LlmRegistry."""
        return [r"gpt-.*", r"o1-.*"]

    def _create_http_client(self) -> Optional[httpx.AsyncClient]:
        """Create HTTP client with custom SSL context using OpenAI SDK defaults.

        Uses DefaultAsyncHttpxClient to preserve OpenAI's default settings for
        timeout, connection pooling, and redirect behavior while applying custom
        SSL configuration.

        Returns:
            DefaultAsyncHttpxClient with SSL configuration, or None if no TLS config
        """
        return self._httpx_async_client_if_tls(DefaultAsyncHttpxClient)

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

        # Refresh token-exchange credential before every call (no-op when not configured).
        if self.token_exchange is not None:
            try:
                self.set_passthrough_key(await self.token_exchange.get_token())
            except Exception as exc:
                yield LlmResponse(error_message=f"Failed to refresh token-exchange credential: {exc}")
                return

        if self.api_format == OPENAI_API_FORMAT_RESPONSES:
            generator = self._generate_content_responses_async(llm_request, stream)
        else:
            generator = self._generate_content_completions_async(llm_request, stream)

        async for response in generator:
            yield response

    async def _generate_content_completions_async(
        self, llm_request: LlmRequest, stream: bool
    ) -> AsyncGenerator[LlmResponse, None]:
        """Generate content using the OpenAI Chat Completions API (/v1/chat/completions)."""
        # Convert messages
        system_instruction = _extract_system_instruction(llm_request)

        messages = _convert_content_to_openai_messages(llm_request.contents, system_instruction)

        # Prepare request parameters
        kwargs = {
            "model": llm_request.model or self.model,
            "messages": messages,
        }

        if self.frequency_penalty is not None:
            kwargs["frequency_penalty"] = self.frequency_penalty
        # max_tokens and max_completion_tokens are mutually exclusive on the
        # OpenAI API: reasoning models (GPT-5 / o-series) reject max_tokens,
        # while some OpenAI-compatible endpoints only accept max_tokens. Never
        # send both; max_completion_tokens (the modern, superset parameter)
        # takes precedence when both are configured.
        if self.max_completion_tokens:
            kwargs["max_completion_tokens"] = self.max_completion_tokens
        elif self.max_tokens:
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
                aggregated_text = ""
                finish_reason = None
                usage_metadata = None
                # Accumulate tool calls - keyed by index since they arrive in chunks
                tool_calls_acc: dict[int, dict[str, Any]] = {}

                # Request usage metadata in streaming mode (OpenAI API feature since Nov 2023)
                # Without this option, chunk.usage is always None in streaming responses
                async for chunk in await self._client.chat.completions.create(
                    stream=True, stream_options={"include_usage": True}, **kwargs
                ):
                    if chunk.choices and chunk.choices[0].delta:
                        delta = chunk.choices[0].delta

                        # Handle text content streaming
                        if delta.content:
                            aggregated_text += delta.content
                            content = types.Content(role="model", parts=[types.Part.from_text(text=delta.content)])
                            yield LlmResponse(
                                content=content, partial=True, turn_complete=chunk.choices[0].finish_reason is not None
                            )

                        # Handle tool call chunks - accumulate them
                        if hasattr(delta, "tool_calls") and delta.tool_calls:
                            for tool_call_chunk in delta.tool_calls:
                                idx = tool_call_chunk.index
                                if idx not in tool_calls_acc:
                                    tool_calls_acc[idx] = {
                                        "id": "",
                                        "name": "",
                                        "arguments": "",
                                        "thought_signature": None,
                                    }
                                # Accumulate the chunks
                                if tool_call_chunk.id:
                                    tool_calls_acc[idx]["id"] = tool_call_chunk.id
                                if tool_call_chunk.function:
                                    if tool_call_chunk.function.name:
                                        tool_calls_acc[idx]["name"] = tool_call_chunk.function.name
                                    if tool_call_chunk.function.arguments:
                                        tool_calls_acc[idx]["arguments"] += tool_call_chunk.function.arguments
                                thought_signature = _extract_thought_signature(
                                    getattr(tool_call_chunk, "model_extra", {}).get("extra_content")
                                )
                                if thought_signature:
                                    tool_calls_acc[idx]["thought_signature"] = thought_signature

                        if chunk.choices[0].finish_reason:
                            finish_reason = chunk.choices[0].finish_reason

                    if hasattr(chunk, "usage") and chunk.usage:
                        usage_metadata = types.GenerateContentResponseUsageMetadata(
                            prompt_token_count=chunk.usage.prompt_tokens,
                            candidates_token_count=chunk.usage.completion_tokens,
                            total_token_count=chunk.usage.total_tokens,
                        )

                # Yield final aggregated response with partial=False
                final_parts = []

                # Add aggregated text if any
                if aggregated_text:
                    final_parts.append(types.Part.from_text(text=aggregated_text))

                # Add accumulated tool calls
                for idx in sorted(tool_calls_acc.keys()):
                    tc = tool_calls_acc[idx]
                    try:
                        args = json.loads(tc["arguments"]) if tc["arguments"] else {}
                    except json.JSONDecodeError:
                        args = {}

                    part = _build_function_call_part(
                        name=tc["name"],
                        args=args,
                        tool_call_id=tc["id"],
                        thought_signature=tc["thought_signature"],
                    )
                    final_parts.append(part)

                # Map finish reason
                final_reason = types.FinishReason.STOP
                if finish_reason == "length":
                    final_reason = types.FinishReason.MAX_TOKENS
                elif finish_reason == "content_filter":
                    final_reason = types.FinishReason.SAFETY
                elif finish_reason == "tool_calls":
                    final_reason = types.FinishReason.STOP  # Tool calls is a normal completion

                # Always yield final response to signal completion and valid metadata
                final_content = types.Content(role="model", parts=final_parts)
                yield LlmResponse(
                    content=final_content,
                    partial=False,
                    finish_reason=final_reason,
                    usage_metadata=usage_metadata,
                    turn_complete=True,
                )
            else:
                # Handle non-streaming
                response = await self._client.chat.completions.create(stream=False, **kwargs)
                yield _convert_openai_response_to_llm_response(response)

        except Exception as e:
            yield LlmResponse(error_code="API_ERROR", error_message=str(e))

    async def _generate_content_responses_async(
        self, llm_request: LlmRequest, stream: bool
    ) -> AsyncGenerator[LlmResponse, None]:
        """Generate content using the OpenAI Responses API (/v1/responses)."""
        system_instruction = _extract_system_instruction(llm_request)
        input_items = _convert_content_to_responses_input(llm_request.contents)

        kwargs: dict[str, Any] = {
            "model": llm_request.model or self.model,
            "input": input_items,
        }
        if system_instruction:
            kwargs["instructions"] = system_instruction

        if self.temperature is not None:
            kwargs["temperature"] = self.temperature
        # Responses uses max_output_tokens (same semantics as max_completion_tokens).
        if self.max_completion_tokens:
            kwargs["max_output_tokens"] = self.max_completion_tokens
        elif self.max_tokens:
            kwargs["max_output_tokens"] = self.max_tokens
        if self.top_p is not None:
            kwargs["top_p"] = self.top_p
        if self.reasoning_effort is not None:
            kwargs["reasoning"] = {"effort": self.reasoning_effort}

        if llm_request.config and llm_request.config.tools:
            genai_tools = [tool for tool in llm_request.config.tools if hasattr(tool, "function_declarations")]
            if genai_tools:
                responses_tools = _convert_tools_to_responses(genai_tools)
                if responses_tools:
                    kwargs["tools"] = responses_tools
                    kwargs["tool_choice"] = "auto"

        try:
            if stream:
                aggregated_text = ""
                status: Optional[str] = None
                usage: Optional[ResponseUsage] = None
                tool_calls: dict[str, types.Part] = {}
                tool_call_order: list[str] = []

                async for event in await self._client.responses.create(stream=True, **kwargs):
                    event_type = getattr(event, "type", None)
                    if event_type == "response.output_text.delta":
                        delta = event.delta
                        if not delta:
                            continue
                        aggregated_text += delta
                        yield LlmResponse(
                            content=types.Content(role="model", parts=[types.Part.from_text(text=delta)]),
                            partial=True,
                            turn_complete=False,
                        )
                    elif event_type == "response.output_item.done":
                        item = event.item
                        if isinstance(item, ResponseFunctionToolCall):
                            call_id = item.call_id or item.id or "call_1"
                            if call_id not in tool_calls:
                                tool_call_order.append(call_id)
                            tool_calls[call_id] = _responses_tool_call_part(item)
                        elif isinstance(item, ResponseOutputMessage) and not aggregated_text:
                            # Endpoints that emit no text deltas still report the text here.
                            aggregated_text += "".join(_responses_output_message_texts(item))
                    elif event_type == "response.completed":
                        usage = event.response.usage
                        status = event.response.status
                    elif event_type == "response.incomplete":
                        usage = event.response.usage
                        status = "incomplete"
                    elif event_type == "response.failed":
                        yield _responses_failure_llm_response(getattr(event, "response", None)) or LlmResponse(
                            error_code="API_ERROR", error_message="OpenAI responses stream failed"
                        )
                        return
                    elif event_type == "error":
                        # Bare error events carry the message directly, not a Response.
                        yield LlmResponse(
                            error_code="API_ERROR",
                            error_message=_responses_error_message(
                                getattr(event, "code", None),
                                getattr(event, "message", None),
                            ),
                        )
                        return

                final_parts: list[types.Part] = []
                if aggregated_text:
                    final_parts.append(types.Part.from_text(text=aggregated_text))
                for call_id in tool_call_order:
                    final_parts.append(tool_calls[call_id])

                yield LlmResponse(
                    content=types.Content(role="model", parts=final_parts),
                    partial=False,
                    turn_complete=True,
                    finish_reason=_responses_status_to_finish_reason(status),
                    usage_metadata=_responses_usage_to_genai(usage),
                )
            else:
                response = await self._client.responses.create(stream=False, **kwargs)
                yield _responses_failure_llm_response(response) or _convert_responses_output_to_llm_response(response)

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
