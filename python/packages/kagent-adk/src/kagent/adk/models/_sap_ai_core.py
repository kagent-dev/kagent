"""SAP AI Core model implementation via Orchestration Service."""

from __future__ import annotations

import json
import logging
import os
import time
from typing import TYPE_CHECKING, Any, AsyncGenerator, Optional

import httpx
from google.adk.models import BaseLlm
from google.adk.models.llm_response import LlmResponse
from google.genai import types

from ._openai import _convert_tools_to_openai
from ._ssl import KAgentTLSMixin

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest

logger = logging.getLogger(__name__)


async def _fetch_oauth_token(
    auth_url: str,
    client_id: str,
    client_secret: str,
    client: httpx.AsyncClient,
) -> tuple[str, float]:
    """Fetch a new OAuth2 token from the auth server. No caching — callers manage expiry."""
    token_url = auth_url.rstrip("/")
    if not token_url.endswith("/oauth/token"):
        token_url += "/oauth/token"

    resp = await client.post(
        token_url,
        data={
            "grant_type": "client_credentials",
            "client_id": client_id,
            "client_secret": client_secret,
        },
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        timeout=30,
    )
    resp.raise_for_status()
    data = resp.json()
    token = data["access_token"]
    expires_at = time.time() + data.get("expires_in", 43200)
    return token, expires_at


def _build_orchestration_template(
    messages: list[types.Content],
    system_instruction: Optional[str] = None,
) -> list[dict[str, Any]]:
    template: list[dict[str, Any]] = []
    if system_instruction:
        template.append({"role": "system", "content": system_instruction})

    for content in messages:
        role = "assistant" if content.role in ("model", "assistant") else "user"
        text_parts: list[str] = []
        tool_calls: list[dict[str, Any]] = []
        function_responses: list[tuple[str, str]] = []

        for part in content.parts or []:
            if part.text:
                text_parts.append(part.text)
            elif part.function_call:
                fc = part.function_call
                tc: dict[str, Any] = {
                    "type": "function",
                    "function": {
                        "name": fc.name or "",
                        "arguments": json.dumps(fc.args) if fc.args else "{}",
                    },
                }
                if fc.id:
                    tc["id"] = fc.id
                tool_calls.append(tc)
            elif part.function_response:
                fr = part.function_response
                resp_content = ""
                if fr.response:
                    resp_content = json.dumps(fr.response) if isinstance(fr.response, dict) else str(fr.response)
                function_responses.append((fr.id or fr.name or "", resp_content))

        if tool_calls:
            msg: dict[str, Any] = {"role": "assistant"}
            if text_parts:
                msg["content"] = "\n".join(text_parts)
            else:
                msg["content"] = ""
            msg["tool_calls"] = tool_calls
            template.append(msg)
        elif function_responses:
            if text_parts:
                template.append({"role": role, "content": "\n".join(text_parts)})
            for tool_call_id, resp_content in function_responses:
                template.append(
                    {
                        "role": "tool",
                        "tool_call_id": tool_call_id,
                        "content": resp_content,
                    }
                )
        elif text_parts:
            template.append({"role": role, "content": "\n".join(text_parts)})

    return template


def _build_orchestration_tools(
    tools: list[types.Tool],
) -> list[dict[str, Any]]:
    openai_tools = _convert_tools_to_openai(tools)
    result = []
    for t in openai_tools:
        result.append(
            {
                "type": "function",
                "function": {
                    "name": t["function"]["name"],
                    "description": t["function"].get("description", ""),
                    "parameters": t["function"].get("parameters", {"type": "object", "properties": {}}),
                },
            }
        )
    return result


def _parse_orchestration_chunk(event_data: dict[str, Any]) -> Optional[dict[str, Any]]:
    if "orchestration_result" in event_data:
        return event_data["orchestration_result"]
    if "final_result" in event_data:
        fr = event_data["final_result"]
        if "object" not in fr:
            fr["object"] = "chat.completion.chunk"
        return fr
    if "choices" in event_data and "object" in event_data:
        return event_data
    return None


_RETRYABLE_STATUS_CODES = {401, 403, 404, 502, 503, 504}


class KAgentSAPAICoreLlm(KAgentTLSMixin, BaseLlm):
    """SAP AI Core LLM via Orchestration Service.

    Supports all model families (OpenAI, Anthropic, Gemini, etc.) through
    SAP's unified Orchestration endpoint.
    """

    base_url: Optional[str] = None
    resource_group: str = "default"
    auth_url: Optional[str] = None
    api_key_passthrough: Optional[bool] = None

    _passthrough_key: Optional[str] = None
    _http_client: Optional[httpx.AsyncClient] = None
    _token: Optional[str] = None
    _token_expires_at: float = 0.0
    _deployment_url: Optional[str] = None
    _deployment_url_expires_at: float = 0.0

    model_config = {"arbitrary_types_allowed": True}

    @classmethod
    def supported_models(cls) -> list[str]:
        return []

    def set_passthrough_key(self, token: str) -> None:
        self._passthrough_key = token
        self._http_client = None

    def _get_http_client(self) -> httpx.AsyncClient:
        if self._http_client is not None:
            return self._http_client
        kwargs: dict[str, Any] = {"timeout": 300}
        kwargs.update(self._tls_httpx_kwargs())
        self._http_client = httpx.AsyncClient(**kwargs)
        return self._http_client

    def _invalidate_token(self) -> None:
        self._token = None
        self._token_expires_at = 0.0

    def _invalidate_deployment_url(self) -> None:
        self._deployment_url = None
        self._deployment_url_expires_at = 0.0

    async def _ensure_token(self) -> str:
        if self._passthrough_key:
            return self._passthrough_key

        now = time.time()
        if self._token and now < self._token_expires_at - 120:
            return self._token

        client_id = os.environ.get("SAP_AI_CORE_CLIENT_ID", "")
        client_secret = os.environ.get("SAP_AI_CORE_CLIENT_SECRET", "")

        if self.auth_url and client_id and client_secret:
            token, expires_at = await _fetch_oauth_token(
                self.auth_url,
                client_id,
                client_secret,
                self._get_http_client(),
            )
            self._token = token
            self._token_expires_at = expires_at
            return token
        raise ValueError("SAP AI Core requires auth_url + SAP_AI_CORE_CLIENT_ID/SECRET env vars")

    async def _get_headers(self) -> dict[str, str]:
        token = await self._ensure_token()
        return {
            "Authorization": f"Bearer {token}",
            "AI-Resource-Group": self.resource_group,
            "Content-Type": "application/json",
        }

    async def _resolve_deployment_url(self) -> str:
        now = time.time()
        if self._deployment_url and now < self._deployment_url_expires_at:
            return self._deployment_url

        if not self.base_url:
            raise ValueError("SAP AI Core requires base_url")

        base = self.base_url.rstrip("/")
        headers = await self._get_headers()
        client = self._get_http_client()
        resp = await client.get(f"{base}/v2/lm/deployments", headers=headers)
        resp.raise_for_status()
        deployments = resp.json()

        valid: list[tuple[str, str]] = []
        for dep in deployments.get("resources", []):
            if dep.get("scenarioId") == "orchestration" and dep.get("status") == "RUNNING":
                url = dep.get("deploymentUrl", "")
                created = dep.get("createdAt", "")
                if url:
                    valid.append((url, created))

        if not valid:
            raise ValueError("No running orchestration deployment found in SAP AI Core")

        self._deployment_url = sorted(valid, key=lambda x: x[1], reverse=True)[0][0]
        self._deployment_url_expires_at = now + 3600
        return self._deployment_url

    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        deployment_url = await self._resolve_deployment_url()
        url = f"{deployment_url}/v2/completion"
        headers = await self._get_headers()

        system_instruction = None
        if llm_request.config and llm_request.config.system_instruction:
            si = llm_request.config.system_instruction
            if isinstance(si, str):
                system_instruction = si
            elif hasattr(si, "parts"):
                parts = getattr(si, "parts", None) or []
                text_parts = [p.text for p in parts if hasattr(p, "text") and p.text]
                system_instruction = "\n".join(text_parts) if text_parts else None

        template = _build_orchestration_template(llm_request.contents or [], system_instruction)

        model_params: dict[str, Any] = {}
        if llm_request.config:
            if llm_request.config.temperature is not None:
                model_params["temperature"] = llm_request.config.temperature
            if llm_request.config.max_output_tokens is not None:
                model_params["max_tokens"] = llm_request.config.max_output_tokens
            if llm_request.config.top_p is not None:
                model_params["top_p"] = llm_request.config.top_p

        prompt_config: dict[str, Any] = {"template": template}

        if llm_request.config and llm_request.config.tools:
            genai_tools: list[types.Tool] = [
                t for t in llm_request.config.tools if isinstance(t, types.Tool) and hasattr(t, "function_declarations")
            ]
            if genai_tools:
                orch_tools = _build_orchestration_tools(genai_tools)
                if orch_tools:
                    prompt_config["tools"] = orch_tools

        body: dict[str, Any] = {
            "config": {
                "modules": {
                    "prompt_templating": {
                        "prompt": prompt_config,
                        "model": {
                            "name": llm_request.model or self.model,
                            "params": model_params,
                            "version": "latest",
                        },
                    },
                },
                "stream": {"enabled": stream},
            }
        }

        try:
            if stream:
                async for llm_resp in self._stream_request(url, headers, body):
                    yield llm_resp
            else:
                yield await self._non_stream_request(url, headers, body)
        except httpx.HTTPStatusError as e:
            status = e.response.status_code
            if status in _RETRYABLE_STATUS_CODES:
                if status in (401, 403):
                    self._invalidate_token()
                self._invalidate_deployment_url()
                logger.warning(
                    "SAP AI Core returned %d from %s, invalidated caches. Retrying once.", status, e.response.url
                )
                try:
                    headers = await self._get_headers()
                    deployment_url = await self._resolve_deployment_url()
                    url = f"{deployment_url}/v2/completion"
                    if stream:
                        async for llm_resp in self._stream_request(url, headers, body):
                            yield llm_resp
                    else:
                        yield await self._non_stream_request(url, headers, body)
                except Exception as retry_err:
                    logger.error("SAP AI Core retry failed: %s", retry_err)
                    yield LlmResponse(error_code="API_ERROR", error_message=str(retry_err))
            else:
                logger.error("SAP AI Core error: %s", e)
                yield LlmResponse(error_code="API_ERROR", error_message=str(e))
        except Exception as e:
            logger.error("SAP AI Core Orchestration error: %s", e)
            yield LlmResponse(error_code="API_ERROR", error_message=str(e))

    async def _stream_request(
        self, url: str, headers: dict[str, str], body: dict[str, Any]
    ) -> AsyncGenerator[LlmResponse, None]:
        aggregated_text = ""
        tool_calls_acc: dict[int, dict[str, Any]] = {}
        finish_reason_str: Optional[str] = None
        usage_metadata: Optional[types.GenerateContentResponseUsageMetadata] = None

        client = self._get_http_client()
        async with client.stream("POST", url, headers=headers, json=body) as resp:
            resp.raise_for_status()
            async for line in resp.aiter_lines():
                line = line.strip()
                if not line:
                    continue

                payload = line[len("data: ") :] if line.startswith("data: ") else line
                if payload == "[DONE]":
                    break

                try:
                    event = json.loads(payload)
                except json.JSONDecodeError:
                    continue

                if "code" in event or "error" in event:
                    raise RuntimeError(json.dumps(event))

                chunk = _parse_orchestration_chunk(event)
                if not chunk:
                    continue

                for choice in chunk.get("choices", []):
                    delta = choice.get("delta", {})
                    if delta.get("content"):
                        text = delta["content"]
                        aggregated_text += text
                        yield LlmResponse(
                            content=types.Content(role="model", parts=[types.Part.from_text(text=text)]),
                            partial=True,
                            turn_complete=False,
                        )

                    if delta.get("tool_calls"):
                        for tc in delta["tool_calls"]:
                            idx = tc.get("index", 0)
                            if idx not in tool_calls_acc:
                                tool_calls_acc[idx] = {"id": "", "name": "", "arguments": ""}
                            if tc.get("id"):
                                tool_calls_acc[idx]["id"] = tc["id"]
                            func = tc.get("function", {})
                            if func.get("name"):
                                tool_calls_acc[idx]["name"] = func["name"]
                            if func.get("arguments"):
                                tool_calls_acc[idx]["arguments"] += func["arguments"]

                    if choice.get("finish_reason"):
                        finish_reason_str = choice["finish_reason"]

                usage = chunk.get("usage")
                if usage:
                    usage_metadata = types.GenerateContentResponseUsageMetadata(
                        prompt_token_count=usage.get("prompt_tokens"),
                        candidates_token_count=usage.get("completion_tokens"),
                        total_token_count=usage.get("total_tokens"),
                    )

        final_parts: list[types.Part] = []
        if aggregated_text:
            final_parts.append(types.Part.from_text(text=aggregated_text))
        for idx in sorted(tool_calls_acc.keys()):
            tc = tool_calls_acc[idx]
            try:
                args = json.loads(tc["arguments"]) if tc["arguments"] else {}
            except json.JSONDecodeError:
                args = {}
            part = types.Part.from_function_call(name=tc["name"], args=args)
            if part.function_call:
                part.function_call.id = tc["id"]
            final_parts.append(part)

        fr = self._map_finish_reason(finish_reason_str)

        yield LlmResponse(
            content=types.Content(role="model", parts=final_parts),
            partial=False,
            turn_complete=True,
            finish_reason=fr,
            usage_metadata=usage_metadata,
        )

    async def _non_stream_request(self, url: str, headers: dict[str, str], body: dict[str, Any]) -> LlmResponse:
        client = self._get_http_client()
        resp = await client.post(url, headers=headers, json=body)
        resp.raise_for_status()
        data = resp.json()

        result = data.get("final_result", data)
        parts: list[types.Part] = []

        for choice in result.get("choices", []):
            msg = choice.get("message", {})
            if msg.get("content"):
                parts.append(types.Part.from_text(text=msg["content"]))
            for tc in msg.get("tool_calls", []):
                func = tc.get("function", {})
                try:
                    args = json.loads(func.get("arguments", "{}"))
                except json.JSONDecodeError:
                    args = {}
                part = types.Part.from_function_call(name=func.get("name", ""), args=args)
                if part.function_call:
                    part.function_call.id = tc.get("id", "")
                parts.append(part)

        usage = result.get("usage", {})
        usage_metadata = (
            types.GenerateContentResponseUsageMetadata(
                prompt_token_count=usage.get("prompt_tokens"),
                candidates_token_count=usage.get("completion_tokens"),
                total_token_count=usage.get("total_tokens"),
            )
            if usage
            else None
        )

        stop_reason = result.get("choices", [{}])[0].get("finish_reason", "stop")
        fr = self._map_finish_reason(stop_reason)

        return LlmResponse(
            content=types.Content(role="model", parts=parts),
            finish_reason=fr,
            usage_metadata=usage_metadata,
        )

    @staticmethod
    def _map_finish_reason(reason: Optional[str]) -> types.FinishReason:
        if reason == "length":
            return types.FinishReason.MAX_TOKENS
        if reason == "content_filter":
            return types.FinishReason.SAFETY
        if reason == "tool_calls":
            return types.FinishReason.STOP
        return types.FinishReason.STOP
