"""Ollama model implementation using the native Ollama SDK."""

from __future__ import annotations

import logging
import os
import uuid
from functools import cached_property
from typing import TYPE_CHECKING, AsyncGenerator, AsyncIterator, Literal, Optional

import ollama as ollama_sdk
from google.adk.models import BaseLlm
from google.adk.models.llm_response import LlmResponse
from google.genai import types
from ollama import AsyncClient
from ollama import Message as OllamaMessage

from ._ssl import KAgentTLSMixin

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

logger = logging.getLogger(__name__)


def _done_reason_to_finish_reason(done_reason: str) -> types.FinishReason:
    if done_reason == "length":
        return types.FinishReason.MAX_TOKENS
    return types.FinishReason.STOP


def _convert_content_to_ollama_messages(
    contents: list[types.Content],
    system_instruction: Optional[str] = None,
) -> list[OllamaMessage]:
    messages: list[OllamaMessage] = []

    if system_instruction:
        messages.append(OllamaMessage(role="system", content=system_instruction))

    for content in contents:
        role = "assistant" if content.role in ("model", "assistant") else "user"

        text_parts: list[str] = []
        function_calls = []
        function_responses = []
        images = []

        for part in content.parts or []:
            if part.text:
                text_parts.append(part.text)
            elif part.function_call:
                function_calls.append(part.function_call)
            elif part.function_response:
                function_responses.append(part.function_response)
            elif (
                part.inline_data
                and part.inline_data.data
                and getattr(part.inline_data, "mime_type", None)
                and str(part.inline_data.mime_type).startswith("image/")
            ):
                images.append(part.inline_data.data)

        if function_calls:
            tool_calls = [
                OllamaMessage.ToolCall(
                    function=OllamaMessage.ToolCall.Function(
                        name=fc.name or "",
                        arguments=fc.args or {},
                    )
                )
                for fc in function_calls
            ]
            messages.append(OllamaMessage(role="assistant", tool_calls=tool_calls))

        if function_responses:
            for fr in function_responses:
                messages.append(
                    OllamaMessage(
                        role="tool",
                        content=_extract_response_content(fr.response),
                        tool_name=fr.name or "",
                    )
                )
        elif text_parts or images:
            msg = OllamaMessage(role=role, content="\n".join(text_parts) or None)
            if images:
                msg.images = images
            messages.append(msg)

    return messages


def _extract_response_content(response: object) -> str:
    if isinstance(response, str):
        return response
    if isinstance(response, dict):
        if "content" in response:
            return "\n".join(item["text"] for item in response["content"] if "text" in item)
        if "result" in response:
            return str(response["result"])
    return ""


def _convert_tools_to_ollama(tools: list[types.Tool]) -> list[ollama_sdk.Tool]:
    ollama_tools = []
    for tool in tools:
        for func_decl in tool.function_declarations or []:
            properties = {}
            required = []
            if func_decl.parameters:
                for prop_name, prop_schema in (func_decl.parameters.properties or {}).items():
                    value_dict = prop_schema.model_dump(exclude_none=True)
                    if "type" in value_dict:
                        value_dict["type"] = value_dict["type"].lower()
                    properties[prop_name] = value_dict
                required = func_decl.parameters.required or []

            ollama_tools.append(
                ollama_sdk.Tool(
                    type="function",
                    function=ollama_sdk.Tool.Function(
                        name=func_decl.name or "",
                        description=func_decl.description or "",
                        parameters=ollama_sdk.Tool.Function.Parameters(
                            type="object",
                            properties=properties,
                            required=required,
                        ),
                    ),
                )
            )
    return ollama_tools


class KAgentOllamaLlm(KAgentTLSMixin, BaseLlm):
    """Ollama model via the native Ollama SDK.

    All Ollama options (temperature, top_p, top_k, num_ctx, etc.) are forwarded
    directly to the Ollama server via the native SDK's options dict.

    The Ollama server host is read from the ``OLLAMA_API_BASE`` environment
    variable (set by the kagent controller from ModelConfig.ollama.host).
    Falls back to ``http://localhost:11434`` if the variable is not set.

    Ollama does not require an API key; api_key_passthrough is accepted in the
    config for interface compatibility but has no effect.
    """

    type: Literal["ollama"] = "ollama"
    ollama_options: Optional[dict[str, object]] = None
    default_headers: Optional[dict[str, str]] = None
    api_key_passthrough: Optional[bool] = None

    @cached_property
    def _client(self) -> AsyncClient:
        host = os.environ.get("OLLAMA_API_BASE", "http://localhost:11434")
        kwargs: dict[str, object] = {
            "host": host,
            "headers": self.default_headers or {},
        }

        kwargs.update(self._tls_httpx_kwargs())

        return AsyncClient(**kwargs)

    @classmethod
    def supported_models(cls) -> list[str]:
        return []

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        system_instruction = None
        if llm_request.config and llm_request.config.system_instruction:
            si = llm_request.config.system_instruction
            if isinstance(si, str):
                system_instruction = si
            elif hasattr(si, "parts"):
                system_instruction = "\n".join(p.text for p in si.parts or [] if p.text)

        messages = _convert_content_to_ollama_messages(llm_request.contents, system_instruction)

        tools = None
        if llm_request.config and llm_request.config.tools:
            genai_tools = [t for t in llm_request.config.tools if hasattr(t, "function_declarations")]
            if genai_tools:
                tools = _convert_tools_to_ollama(genai_tools) or None

        try:
            if stream:
                aggregated_text = ""
                response: AsyncIterator[ollama_sdk.ChatResponse] = await self._client.chat(
                    model=llm_request.model or self.model,
                    messages=messages,
                    tools=tools,
                    options=self.ollama_options or None,
                    stream=True,
                )
                async for chunk in response:
                    if chunk.message.content:
                        aggregated_text += chunk.message.content
                        yield LlmResponse(
                            content=types.Content(
                                role="model", parts=[types.Part.from_text(text=chunk.message.content)]
                            ),
                            partial=True,
                            turn_complete=False,
                        )
                    if chunk.done:
                        final_parts = []
                        if aggregated_text:
                            final_parts.append(types.Part.from_text(text=aggregated_text))
                        for tc in chunk.message.tool_calls or []:
                            part = types.Part.from_function_call(
                                name=tc.function.name, args=dict(tc.function.arguments)
                            )
                            if part.function_call:
                                part.function_call.id = str(uuid.uuid4())
                            final_parts.append(part)
                        finish_reason = _done_reason_to_finish_reason(chunk.done_reason) if chunk.done_reason else None
                        usage_metadata = None
                        if chunk.prompt_eval_count is not None or chunk.eval_count is not None:
                            usage_metadata = types.GenerateContentResponseUsageMetadata(
                                prompt_token_count=chunk.prompt_eval_count,
                                candidates_token_count=chunk.eval_count,
                                total_token_count=(chunk.prompt_eval_count or 0) + (chunk.eval_count or 0),
                            )
                        yield LlmResponse(
                            content=types.Content(role="model", parts=final_parts),
                            partial=False,
                            turn_complete=True,
                            finish_reason=finish_reason,
                            usage_metadata=usage_metadata,
                        )
            else:
                response = await self._client.chat(
                    model=llm_request.model or self.model,
                    messages=messages,
                    tools=tools,
                    options=self.ollama_options or None,
                    stream=False,
                )
                parts = []
                if response.message.content:
                    parts.append(types.Part.from_text(text=response.message.content))
                for tc in response.message.tool_calls or []:
                    part = types.Part.from_function_call(name=tc.function.name, args=dict(tc.function.arguments))
                    if part.function_call:
                        part.function_call.id = str(uuid.uuid4())
                    parts.append(part)
                finish_reason = _done_reason_to_finish_reason(response.done_reason) if response.done_reason else None
                usage_metadata = None
                if response.prompt_eval_count is not None or response.eval_count is not None:
                    usage_metadata = types.GenerateContentResponseUsageMetadata(
                        prompt_token_count=response.prompt_eval_count,
                        candidates_token_count=response.eval_count,
                        total_token_count=(response.prompt_eval_count or 0) + (response.eval_count or 0),
                    )
                yield LlmResponse(
                    content=types.Content(role="model", parts=parts),
                    finish_reason=finish_reason,
                    usage_metadata=usage_metadata,
                )

        except Exception as e:
            yield LlmResponse(error_code="API_ERROR", error_message=str(e))


def create_ollama_llm(
    model: str,
    options: dict[str, object] | None,
    extra_headers: dict[str, str],
    tls_disable_verify: Optional[bool] = None,
    tls_ca_cert_path: Optional[str] = None,
    tls_disable_system_cas: Optional[bool] = None,
    api_key_passthrough: Optional[bool] = None,
) -> KAgentOllamaLlm:
    """Build a KAgentOllamaLlm from Ollama options.

    All options (including native Ollama parameters like num_ctx, top_k, etc.)
    are forwarded directly to the Ollama SDK. Type coercion from the CRD string
    values is expected to be done by the caller (see _convert_ollama_options).
    """
    return KAgentOllamaLlm(
        model=model,
        ollama_options=options or None,
        default_headers=extra_headers or {},
        tls_disable_verify=tls_disable_verify,
        tls_ca_cert_path=tls_ca_cert_path,
        tls_disable_system_cas=tls_disable_system_cas,
        api_key_passthrough=api_key_passthrough,
    )
