"""AWS Bedrock model implementation using the Bedrock Converse API.

Uses boto3's Converse API which provides a consistent interface across all
Bedrock-supported models (Anthropic, Meta, Mistral, Amazon, Cohere, etc.).
Authenticates via the standard AWS credential chain (env vars, IAM role, etc.).
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
from typing import TYPE_CHECKING, Any, AsyncGenerator, Optional

import boto3
from google.adk.models import BaseLlm
from google.adk.models.llm_response import LlmResponse
from google.genai import types

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

logger = logging.getLogger(__name__)


def _get_bedrock_client(extra_headers: Optional[dict[str, str]] = None):
    region = os.environ.get("AWS_DEFAULT_REGION") or os.environ.get("AWS_REGION") or "us-east-1"
    kwargs: dict[str, Any] = {"region_name": region}
    if extra_headers:
        # boto3 doesn't support custom headers natively; log and ignore
        logger.warning("extra_headers are not supported for Bedrock models and will be ignored.")
    return boto3.client("bedrock-runtime", **kwargs)


def _convert_content_to_converse_messages(contents: list[types.Content]) -> list[dict]:
    messages = []
    for content in contents:
        role = "assistant" if content.role in ("model", "assistant") else "user"
        blocks = []

        for part in content.parts or []:
            if part.text:
                blocks.append({"text": part.text})
            elif part.function_call:
                blocks.append(
                    {
                        "toolUse": {
                            "toolUseId": part.function_call.id or "",
                            "name": part.function_call.name or "",
                            "input": part.function_call.args or {},
                        }
                    }
                )
            elif part.function_response:
                content_block = _extract_tool_result_content(part.function_response.response)
                blocks.append(
                    {
                        "toolResult": {
                            "toolUseId": part.function_response.id or "",
                            "content": content_block,
                        }
                    }
                )
            elif part.inline_data and part.inline_data.data and part.inline_data.mime_type:
                media_type, _, fmt = part.inline_data.mime_type.partition("/")
                if media_type == "image":
                    blocks.append(
                        {
                            "image": {
                                "format": fmt,
                                "source": {"bytes": part.inline_data.data},
                            }
                        }
                    )

        if blocks:
            messages.append({"role": role, "content": blocks})

    return messages


def _extract_tool_result_content(response: object) -> list[dict]:
    if isinstance(response, str):
        return [{"text": response}]
    if isinstance(response, dict):
        if "content" in response:
            text = "\n".join(item["text"] for item in response["content"] if "text" in item)
            return [{"text": text}]
        if "result" in response:
            return [{"text": str(response["result"])}]
    return [{"text": str(response)}]


def _convert_tools_to_converse(tools: list[types.Tool]) -> list[dict]:
    converse_tools = []
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

            converse_tools.append(
                {
                    "toolSpec": {
                        "name": func_decl.name or "",
                        "description": func_decl.description or "",
                        "inputSchema": {
                            "json": {
                                "type": "object",
                                "properties": properties,
                                "required": required,
                            }
                        },
                    }
                }
            )
    return converse_tools


def _stop_reason_to_finish_reason(stop_reason: str) -> types.FinishReason:
    if stop_reason == "max_tokens":
        return types.FinishReason.MAX_TOKENS
    if stop_reason in ("content_filtered", "guardrail_intervened"):
        return types.FinishReason.SAFETY
    return types.FinishReason.STOP


class KAgentBedrockLlm(BaseLlm):
    """Bedrock model via the Converse API.

    Supports all Bedrock-compatible models (Anthropic, Meta, Mistral, Amazon, etc.).
    Authenticates using the standard AWS credential chain.
    """

    extra_headers: Optional[dict[str, str]] = None
    model_config = {"arbitrary_types_allowed": True}

    @classmethod
    def supported_models(cls) -> list[str]:
        return []

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        client = _get_bedrock_client(self.extra_headers)
        model_id = llm_request.model or self.model

        messages = _convert_content_to_converse_messages(llm_request.contents or [])

        kwargs: dict[str, Any] = {"modelId": model_id, "messages": messages}

        if llm_request.config and llm_request.config.system_instruction:
            si = llm_request.config.system_instruction
            if isinstance(si, str):
                kwargs["system"] = [{"text": si}]
            elif hasattr(si, "parts"):
                text = "\n".join(p.text for p in si.parts or [] if p.text)
                if text:
                    kwargs["system"] = [{"text": text}]

        if llm_request.config and llm_request.config.tools:
            genai_tools = [t for t in llm_request.config.tools if hasattr(t, "function_declarations")]
            if genai_tools:
                converse_tools = _convert_tools_to_converse(genai_tools)
                if converse_tools:
                    kwargs["toolConfig"] = {"tools": converse_tools}

        try:
            if stream:
                response = await asyncio.to_thread(client.converse_stream, **kwargs)
                stream_body = response.get("stream", [])

                aggregated_text = ""
                tool_uses: dict[str, dict] = {}  # toolUseId -> {name, input_json}
                current_tool_id: Optional[str] = None
                stop_reason = "end_turn"
                usage_metadata: Optional[types.GenerateContentResponseUsageMetadata] = None

                for event in stream_body:
                    if "contentBlockStart" in event:
                        start = event["contentBlockStart"].get("start", {})
                        if "toolUse" in start:
                            current_tool_id = start["toolUse"]["toolUseId"]
                            tool_uses[current_tool_id] = {
                                "name": start["toolUse"]["name"],
                                "input_json": "",
                            }

                    elif "contentBlockDelta" in event:
                        delta = event["contentBlockDelta"].get("delta", {})
                        if "text" in delta:
                            aggregated_text += delta["text"]
                            yield LlmResponse(
                                content=types.Content(role="model", parts=[types.Part.from_text(text=delta["text"])]),
                                partial=True,
                                turn_complete=False,
                            )
                        elif "toolUse" in delta and current_tool_id:
                            tool_uses[current_tool_id]["input_json"] += delta["toolUse"].get("input", "")

                    elif "messageStop" in event:
                        stop_reason = event["messageStop"].get("stopReason", "end_turn")

                    elif "metadata" in event:
                        usage = event["metadata"].get("usage", {})
                        if usage:
                            usage_metadata = types.GenerateContentResponseUsageMetadata(
                                prompt_token_count=usage.get("inputTokens"),
                                candidates_token_count=usage.get("outputTokens"),
                                total_token_count=usage.get("totalTokens"),
                            )

                final_parts = []
                if aggregated_text:
                    final_parts.append(types.Part.from_text(text=aggregated_text))
                for tool_id, tool in tool_uses.items():
                    args = json.loads(tool["input_json"]) if tool["input_json"] else {}
                    part = types.Part.from_function_call(name=tool["name"], args=args)
                    if part.function_call:
                        part.function_call.id = tool_id
                    final_parts.append(part)

                yield LlmResponse(
                    content=types.Content(role="model", parts=final_parts),
                    partial=False,
                    turn_complete=True,
                    finish_reason=_stop_reason_to_finish_reason(stop_reason),
                    usage_metadata=usage_metadata,
                )

            else:
                response = await asyncio.to_thread(client.converse, **kwargs)
                output_message = response.get("output", {}).get("message", {})
                stop_reason = response.get("stopReason", "end_turn")

                parts = []
                for block in output_message.get("content", []):
                    if "text" in block:
                        parts.append(types.Part.from_text(text=block["text"]))
                    elif "toolUse" in block:
                        tool = block["toolUse"]
                        part = types.Part.from_function_call(name=tool["name"], args=tool.get("input", {}))
                        if part.function_call:
                            part.function_call.id = tool["toolUseId"]
                        parts.append(part)

                usage = response.get("usage", {})
                usage_metadata = types.GenerateContentResponseUsageMetadata(
                    prompt_token_count=usage.get("inputTokens"),
                    candidates_token_count=usage.get("outputTokens"),
                    total_token_count=usage.get("totalTokens"),
                )

                yield LlmResponse(
                    content=types.Content(role="model", parts=parts),
                    finish_reason=_stop_reason_to_finish_reason(stop_reason),
                    usage_metadata=usage_metadata,
                )

        except Exception as e:
            yield LlmResponse(error_code="API_ERROR", error_message=str(e))
