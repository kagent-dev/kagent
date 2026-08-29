"""OpenAI Responses API path for BaseOpenAI (OpenAI and Azure OpenAI)."""

from __future__ import annotations

import base64
import json
from typing import TYPE_CHECKING, Any, AsyncGenerator, Optional

from google.adk.models.llm_response import LlmResponse
from google.genai import types
from google.genai.types import FunctionCall, FunctionResponse

from ._utils import function_declaration_schema

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

    from ._openai import BaseOpenAI


def _system_instruction(llm_request: LlmRequest) -> Optional[str]:
    if not llm_request.config or not llm_request.config.system_instruction:
        return None
    system_instruction = llm_request.config.system_instruction
    if isinstance(system_instruction, str):
        return system_instruction
    if hasattr(system_instruction, "parts"):
        text_parts = []
        for part in getattr(system_instruction, "parts", []) or []:
            if hasattr(part, "text") and part.text:
                text_parts.append(part.text)
        return "\n".join(text_parts) if text_parts else None
    return None


def _function_response_output(func_response: FunctionResponse) -> str:
    if isinstance(func_response.response, str):
        return func_response.response
    if func_response.response and "content" in func_response.response:
        content_list = func_response.response["content"]
        if content_list:
            return "\n".join(item["text"] for item in content_list if "text" in item)
    if func_response.response and "result" in func_response.response:
        return str(func_response.response["result"])
    return ""


def contents_to_responses_input(contents: list[types.Content]) -> list[dict[str, Any]]:
    """Convert google.genai Content list to Responses API input items."""
    function_responses: dict[str, FunctionResponse] = {}
    for content in contents:
        for part in content.parts or []:
            if part.function_response:
                tool_call_id = part.function_response.id or "call_1"
                function_responses[tool_call_id] = part.function_response

    items: list[dict[str, Any]] = []
    for content in contents:
        role = content.role or "user"
        if role == "system":
            continue

        text_parts: list[str] = []
        function_calls: list[FunctionCall] = []
        image_urls: list[str] = []
        for part in content.parts or []:
            if part.text:
                text_parts.append(part.text)
            elif part.function_call:
                function_calls.append(part.function_call)
            elif part.inline_data and part.inline_data.mime_type and part.inline_data.mime_type.startswith("image"):
                if part.inline_data.data:
                    image_data = base64.b64encode(part.inline_data.data).decode()
                    image_urls.append(f"data:{part.inline_data.mime_type};base64,{image_data}")

        if function_calls and role in ("model", "assistant"):
            if text_parts:
                items.append({"role": "assistant", "content": "\n".join(text_parts)})
            for func_call in function_calls:
                tool_call_id = func_call.id or "call_1"
                items.append(
                    {
                        "type": "function_call",
                        "call_id": tool_call_id,
                        "name": func_call.name or "",
                        "arguments": json.dumps(func_call.args) if func_call.args else "{}",
                    }
                )
                output = "No response available for this function call."
                if tool_call_id in function_responses:
                    output = _function_response_output(function_responses[tool_call_id]) or output
                items.append({"type": "function_call_output", "call_id": tool_call_id, "output": output})
            continue

        if not text_parts and not image_urls:
            continue

        msg_role = "assistant" if role in ("model", "assistant") else "user"
        if image_urls:
            content_parts: list[dict[str, Any]] = [{"type": "input_text", "text": t} for t in text_parts]
            content_parts.extend({"type": "input_image", "image_url": url} for url in image_urls)
            items.append({"role": msg_role, "content": content_parts})
        else:
            items.append({"role": msg_role, "content": "\n".join(text_parts)})

    return items


def tools_to_responses_tools(tools: list[types.Tool]) -> list[dict[str, Any]]:
    """Convert google.genai Tools to Responses function tools."""
    out: list[dict[str, Any]] = []
    for tool in tools:
        if not tool.function_declarations:
            continue
        for func_decl in tool.function_declarations:
            parameters = function_declaration_schema(func_decl)
            out.append(
                {
                    "type": "function",
                    "name": func_decl.name or "",
                    "description": func_decl.description or "",
                    "parameters": parameters,
                    "strict": False,
                }
            )
    return out


def _usage_metadata(usage: Any) -> Optional[types.GenerateContentResponseUsageMetadata]:
    if usage is None:
        return None
    input_tokens = getattr(usage, "input_tokens", None) or 0
    output_tokens = getattr(usage, "output_tokens", None) or 0
    if input_tokens == 0 and output_tokens == 0:
        return None
    return types.GenerateContentResponseUsageMetadata(
        prompt_token_count=input_tokens,
        candidates_token_count=output_tokens,
        total_token_count=getattr(usage, "total_tokens", None) or (input_tokens + output_tokens),
    )


def _finish_reason(status: Optional[str]) -> types.FinishReason:
    if status == "incomplete":
        return types.FinishReason.MAX_TOKENS
    if status == "failed":
        return types.FinishReason.OTHER
    return types.FinishReason.STOP


def _function_call_part(name: str, arguments: str, call_id: str) -> types.Part:
    try:
        args = json.loads(arguments) if arguments else {}
    except json.JSONDecodeError:
        args = {}
    part = types.Part.from_function_call(name=name, args=args)
    if part.function_call:
        part.function_call.id = call_id
    return part


def response_to_llm_response(response: Any) -> LlmResponse:
    """Convert an OpenAI Responses API result to LlmResponse."""
    parts: list[types.Part] = []
    for item in getattr(response, "output", None) or []:
        item_type = getattr(item, "type", None)
        if item_type == "message":
            for content in getattr(item, "content", None) or []:
                if getattr(content, "type", None) == "output_text" and getattr(content, "text", None):
                    parts.append(types.Part.from_text(text=content.text))
        elif item_type == "function_call":
            call_id = getattr(item, "call_id", None) or getattr(item, "id", None) or "call_1"
            parts.append(
                _function_call_part(
                    getattr(item, "name", "") or "",
                    getattr(item, "arguments", "") or "",
                    call_id,
                )
            )

    return LlmResponse(
        content=types.Content(role="model", parts=parts),
        usage_metadata=_usage_metadata(getattr(response, "usage", None)),
        finish_reason=_finish_reason(getattr(response, "status", None)),
    )


def _build_request_kwargs(model: BaseOpenAI, llm_request: LlmRequest) -> dict[str, Any]:
    kwargs: dict[str, Any] = {
        "model": llm_request.model or model.model,
        "input": contents_to_responses_input(llm_request.contents),
    }
    instructions = _system_instruction(llm_request)
    if instructions:
        kwargs["instructions"] = instructions
    if model.temperature is not None:
        kwargs["temperature"] = model.temperature
    if model.max_completion_tokens:
        kwargs["max_output_tokens"] = model.max_completion_tokens
    elif model.max_tokens:
        kwargs["max_output_tokens"] = model.max_tokens
    if model.top_p is not None:
        kwargs["top_p"] = model.top_p
    if model.reasoning_effort is not None:
        kwargs["reasoning"] = {"effort": model.reasoning_effort}

    if llm_request.config and llm_request.config.tools:
        genai_tools = [tool for tool in llm_request.config.tools if hasattr(tool, "function_declarations")]
        if genai_tools:
            openai_tools = tools_to_responses_tools(genai_tools)
            if openai_tools:
                kwargs["tools"] = openai_tools
                kwargs["tool_choice"] = "auto"
    return kwargs


async def generate_content_responses(
    model: BaseOpenAI, llm_request: LlmRequest, stream: bool
) -> AsyncGenerator[LlmResponse, None]:
    """Generate content using the OpenAI Responses API."""
    kwargs = _build_request_kwargs(model, llm_request)
    try:
        if stream:
            aggregated_text = ""
            tool_calls: dict[str, types.Part] = {}
            tool_call_order: list[str] = []
            usage_metadata = None
            finish_reason = types.FinishReason.STOP

            async for event in await model._client.responses.create(stream=True, **kwargs):
                event_type = getattr(event, "type", None)
                if event_type == "response.output_text.delta":
                    delta = getattr(event, "delta", None) or ""
                    if not delta:
                        continue
                    aggregated_text += delta
                    yield LlmResponse(
                        content=types.Content(role="model", parts=[types.Part.from_text(text=delta)]),
                        partial=True,
                        turn_complete=False,
                    )
                elif event_type == "response.output_item.done":
                    item = getattr(event, "item", None)
                    if getattr(item, "type", None) == "function_call":
                        call_id = getattr(item, "call_id", None) or getattr(item, "id", None) or "call_1"
                        if call_id not in tool_calls:
                            tool_call_order.append(call_id)
                        tool_calls[call_id] = _function_call_part(
                            getattr(item, "name", "") or "",
                            getattr(item, "arguments", "") or "",
                            call_id,
                        )
                elif event_type == "response.completed":
                    completed = getattr(event, "response", None)
                    usage_metadata = _usage_metadata(getattr(completed, "usage", None))
                    finish_reason = _finish_reason(getattr(completed, "status", None))
                elif event_type == "response.incomplete":
                    incomplete = getattr(event, "response", None)
                    usage_metadata = _usage_metadata(getattr(incomplete, "usage", None))
                    finish_reason = types.FinishReason.MAX_TOKENS

            final_parts: list[types.Part] = []
            if aggregated_text:
                final_parts.append(types.Part.from_text(text=aggregated_text))
            final_parts.extend(tool_calls[call_id] for call_id in tool_call_order)
            yield LlmResponse(
                content=types.Content(role="model", parts=final_parts),
                partial=False,
                finish_reason=finish_reason,
                usage_metadata=usage_metadata,
                turn_complete=True,
            )
        else:
            response = await model._client.responses.create(stream=False, **kwargs)
            yield response_to_llm_response(response)
    except Exception as e:
        yield LlmResponse(error_code="API_ERROR", error_message=str(e))
