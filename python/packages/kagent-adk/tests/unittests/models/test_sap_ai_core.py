"""Tests for KAgentSAPAICoreLlm (SAP AI Core via Orchestration Service)."""

import json
import time
from unittest import mock
from unittest.mock import AsyncMock, MagicMock, patch

import httpx
import pytest
from google.genai import types

from kagent.adk.models._sap_ai_core import (
    KAgentSAPAICoreLlm,
    _build_orchestration_template,
    _build_orchestration_tools,
    _fetch_oauth_token,
    _parse_orchestration_chunk,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_llm(base_url="https://api.example.com", auth_url="https://auth.example.com"):
    return KAgentSAPAICoreLlm(
        model="anthropic--claude-3.5-sonnet",
        base_url=base_url,
        resource_group="default",
        auth_url=auth_url,
    )


def _content(role: str, text: str) -> types.Content:
    return types.Content(role=role, parts=[types.Part.from_text(text=text)])


def _sse_body(*payloads: dict) -> AsyncMock:
    """Return a mock httpx streaming response that yields SSE lines."""
    lines = []
    for p in payloads:
        lines.append(f"data: {json.dumps(p)}")
    lines.append("data: [DONE]")

    async def _aiter_lines():
        for line in lines:
            yield line

    resp = MagicMock()
    resp.raise_for_status = MagicMock()
    resp.aiter_lines = _aiter_lines
    return resp


def _make_request(contents=None, model="anthropic--claude-3.5-sonnet", config=None):
    req = MagicMock()
    req.model = model
    req.contents = contents or []
    req.config = config
    return req


# ---------------------------------------------------------------------------
# _build_orchestration_template
# ---------------------------------------------------------------------------


class TestBuildOrchestrationTemplate:
    def test_empty_input(self):
        result = _build_orchestration_template([])
        assert result == []

    def test_system_instruction_prepended(self):
        result = _build_orchestration_template([], system_instruction="Be helpful.")
        assert result[0] == {"role": "system", "content": "Be helpful."}

    def test_user_and_assistant_messages(self):
        contents = [
            _content("user", "Hello"),
            _content("model", "Hi there"),
        ]
        result = _build_orchestration_template(contents)
        assert len(result) == 2
        assert result[0] == {"role": "user", "content": "Hello"}
        assert result[1] == {"role": "assistant", "content": "Hi there"}

    def test_assistant_role_alias(self):
        contents = [_content("assistant", "Reply")]
        result = _build_orchestration_template(contents)
        assert result[0]["role"] == "assistant"

    def test_tool_call_message(self):
        fc_part = types.Part.from_function_call(name="get_weather", args={"city": "Berlin"})
        fc_part.function_call.id = "call_1"
        content = types.Content(role="model", parts=[fc_part])
        result = _build_orchestration_template([content])

        assert len(result) == 1
        msg = result[0]
        assert msg["role"] == "assistant"
        assert msg["content"] == ""
        tool_calls = msg["tool_calls"]
        assert len(tool_calls) == 1
        assert tool_calls[0]["id"] == "call_1"
        assert tool_calls[0]["function"]["name"] == "get_weather"
        assert json.loads(tool_calls[0]["function"]["arguments"]) == {"city": "Berlin"}

    def test_function_response_message(self):
        fr_part = types.Part.from_function_response(name="get_weather", response={"temp": "20C"})
        fr_part.function_response.id = "call_1"
        content = types.Content(role="user", parts=[fr_part])
        result = _build_orchestration_template([content])

        assert len(result) == 1
        assert result[0]["role"] == "tool"
        assert result[0]["tool_call_id"] == "call_1"
        assert "20C" in result[0]["content"]

    def test_mixed_text_and_tool_call(self):
        """Tool call with accompanying text — text goes into content field."""
        fc_part = types.Part.from_function_call(name="search", args={})
        fc_part.function_call.id = "call_2"
        text_part = types.Part.from_text(text="Searching now…")
        content = types.Content(role="model", parts=[text_part, fc_part])
        result = _build_orchestration_template([content])

        assert result[0]["content"] == "Searching now…"
        assert "tool_calls" in result[0]

    def test_empty_parts_skipped(self):
        content = types.Content(role="user", parts=[])
        result = _build_orchestration_template([content])
        assert result == []


# ---------------------------------------------------------------------------
# _build_orchestration_tools
# ---------------------------------------------------------------------------


class TestBuildOrchestrationTools:
    def test_empty_input(self):
        assert _build_orchestration_tools([]) == []

    def test_single_function(self):
        tool = types.Tool(
            function_declarations=[
                types.FunctionDeclaration(
                    name="list_pods",
                    description="List Kubernetes pods",
                    parameters=types.Schema(
                        type=types.Type.OBJECT,
                        properties={"namespace": types.Schema(type=types.Type.STRING)},
                    ),
                )
            ]
        )
        result = _build_orchestration_tools([tool])
        assert len(result) == 1
        fn = result[0]["function"]
        assert fn["name"] == "list_pods"
        assert fn["description"] == "List Kubernetes pods"
        assert "namespace" in fn["parameters"]["properties"]

    def test_multiple_declarations(self):
        tool = types.Tool(
            function_declarations=[
                types.FunctionDeclaration(name="fn_a", description="A"),
                types.FunctionDeclaration(name="fn_b", description="B"),
            ]
        )
        result = _build_orchestration_tools([tool])
        names = [r["function"]["name"] for r in result]
        assert names == ["fn_a", "fn_b"]


# ---------------------------------------------------------------------------
# _parse_orchestration_chunk
# ---------------------------------------------------------------------------


class TestParseOrchestrationChunk:
    def test_orchestration_result_envelope(self):
        event = {"orchestration_result": {"choices": []}}
        result = _parse_orchestration_chunk(event)
        assert result is not None
        assert "choices" in result

    def test_final_result_envelope(self):
        event = {"final_result": {"choices": [], "object": "chat.completion.chunk"}}
        result = _parse_orchestration_chunk(event)
        assert result is not None
        assert "choices" in result

    def test_final_result_adds_object_field(self):
        event = {"final_result": {"choices": []}}
        result = _parse_orchestration_chunk(event)
        assert result["object"] == "chat.completion.chunk"

    def test_direct_choices_with_object(self):
        event = {"choices": [], "object": "chat.completion.chunk"}
        result = _parse_orchestration_chunk(event)
        assert result is event

    def test_unrecognized_returns_none(self):
        assert _parse_orchestration_chunk({"foo": "bar"}) is None


# ---------------------------------------------------------------------------
# OAuth token caching (_ensure_token)
# ---------------------------------------------------------------------------


class TestEnsureToken:
    @pytest.mark.asyncio
    async def test_fetches_token_on_first_call(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        with patch(
            "kagent.adk.models._sap_ai_core._fetch_oauth_token",
            new_callable=AsyncMock,
            return_value=("tok-1", time.time() + 3600),
        ) as mock_fetch:
            token = await llm._ensure_token()

        assert token == "tok-1"
        mock_fetch.assert_awaited_once()

    @pytest.mark.asyncio
    async def test_caches_valid_token(self, monkeypatch):
        llm = _make_llm()
        llm._token = "cached-tok"
        llm._token_expires_at = time.time() + 3600
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        with patch(
            "kagent.adk.models._sap_ai_core._fetch_oauth_token",
            new_callable=AsyncMock,
        ) as mock_fetch:
            token = await llm._ensure_token()

        assert token == "cached-tok"
        mock_fetch.assert_not_awaited()

    @pytest.mark.asyncio
    async def test_refreshes_expired_token(self, monkeypatch):
        llm = _make_llm()
        llm._token = "old-tok"
        llm._token_expires_at = time.time() - 1  # already expired
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        with patch(
            "kagent.adk.models._sap_ai_core._fetch_oauth_token",
            new_callable=AsyncMock,
            return_value=("new-tok", time.time() + 3600),
        ) as mock_fetch:
            token = await llm._ensure_token()

        assert token == "new-tok"
        mock_fetch.assert_awaited_once()

    @pytest.mark.asyncio
    async def test_raises_when_env_vars_missing(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.delenv("SAP_AI_CORE_CLIENT_ID", raising=False)
        monkeypatch.delenv("SAP_AI_CORE_CLIENT_SECRET", raising=False)

        with pytest.raises(ValueError, match="SAP_AI_CORE_CLIENT"):
            await llm._ensure_token()

    def test_set_passthrough_key_invalidates_http_client(self):
        llm = _make_llm()
        # Force creation of the http client.
        client = llm._get_http_client()
        assert client is not None
        assert llm._http_client is not None

        llm.set_passthrough_key("my-bearer-token")

        assert llm._passthrough_key == "my-bearer-token"
        # Client must be cleared so a new one is created with fresh config.
        assert llm._http_client is None

    @pytest.mark.asyncio
    async def test_passthrough_key_skips_oauth(self, monkeypatch):
        llm = _make_llm()
        llm.set_passthrough_key("bearer-pass")
        monkeypatch.delenv("SAP_AI_CORE_CLIENT_ID", raising=False)

        with patch(
            "kagent.adk.models._sap_ai_core._fetch_oauth_token",
            new_callable=AsyncMock,
        ) as mock_fetch:
            token = await llm._ensure_token()

        assert token == "bearer-pass"
        mock_fetch.assert_not_awaited()

    @pytest.mark.asyncio
    async def test_raises_when_auth_url_missing(self, monkeypatch):
        """auth_url=None with no passthrough key should raise ValueError."""
        llm = KAgentSAPAICoreLlm(model="test", base_url="https://api.example.com", auth_url=None)
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        with pytest.raises(ValueError, match="SAP_AI_CORE_CLIENT"):
            await llm._ensure_token()

    def test_invalidate_clears_token(self):
        llm = _make_llm()
        llm._token = "tok"
        llm._token_expires_at = time.time() + 3600
        llm._invalidate_token()
        assert llm._token is None
        assert llm._token_expires_at == 0.0


# ---------------------------------------------------------------------------
# Deployment URL resolution and caching (_resolve_deployment_url)
# ---------------------------------------------------------------------------


class TestResolveDeploymentURL:
    def _dep_response(self, *urls):
        resources = [
            {
                "scenarioId": "orchestration",
                "status": "RUNNING",
                "deploymentUrl": u,
                "createdAt": f"2024-01-{i + 1:02d}T00:00:00Z",
            }
            for i, u in enumerate(urls)
        ]
        return {"resources": resources}

    @pytest.mark.asyncio
    async def test_resolves_and_caches_url(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = self._dep_response("https://dep.example.com")

        with (
            patch.object(llm, "_ensure_token", new_callable=AsyncMock, return_value="tok"),
            patch.object(llm._get_http_client(), "get", new_callable=AsyncMock, return_value=mock_resp) as mock_get,
        ):
            url1 = await llm._resolve_deployment_url()
            url2 = await llm._resolve_deployment_url()

        assert url1 == "https://dep.example.com"
        assert url2 == "https://dep.example.com"
        # Second call must use the cache — HTTP GET called only once.
        mock_get.assert_awaited_once()

    @pytest.mark.asyncio
    async def test_picks_most_recently_created(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = self._dep_response(
            "https://older.example.com",  # createdAt 2024-01-01
            "https://newer.example.com",  # createdAt 2024-01-02
        )

        with (
            patch.object(llm, "_ensure_token", new_callable=AsyncMock, return_value="tok"),
            patch.object(llm._get_http_client(), "get", new_callable=AsyncMock, return_value=mock_resp),
        ):
            url = await llm._resolve_deployment_url()

        assert url == "https://newer.example.com"

    @pytest.mark.asyncio
    async def test_raises_when_no_running_deployment(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = {
            "resources": [{"scenarioId": "other", "status": "RUNNING", "deploymentUrl": "https://x.example.com"}]
        }

        with (
            patch.object(llm, "_ensure_token", new_callable=AsyncMock, return_value="tok"),
            patch.object(llm._get_http_client(), "get", new_callable=AsyncMock, return_value=mock_resp),
        ):
            with pytest.raises(ValueError, match="No running orchestration"):
                await llm._resolve_deployment_url()

    @pytest.mark.asyncio
    async def test_expires_and_refreshes(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = self._dep_response("https://dep.example.com")

        with (
            patch.object(llm, "_ensure_token", new_callable=AsyncMock, return_value="tok"),
            patch.object(llm._get_http_client(), "get", new_callable=AsyncMock, return_value=mock_resp) as mock_get,
        ):
            await llm._resolve_deployment_url()

            # Expire the cache.
            llm._deployment_url_expires_at = time.time() - 1

            await llm._resolve_deployment_url()

        assert mock_get.await_count == 2

    def test_invalidate_clears_url(self):
        llm = _make_llm()
        llm._deployment_url = "https://old.example.com"
        llm._deployment_url_expires_at = time.time() + 3600
        llm._invalidate_deployment_url()
        assert llm._deployment_url is None
        assert llm._deployment_url_expires_at == 0.0


# ---------------------------------------------------------------------------
# _non_stream_request
# ---------------------------------------------------------------------------


class TestNonStreamRequest:
    @pytest.mark.asyncio
    async def test_text_response(self):
        llm = _make_llm()
        data = {
            "final_result": {
                "choices": [{"finish_reason": "stop", "message": {"content": "Hello!"}}],
            }
        }
        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = data

        with patch.object(llm._get_http_client(), "post", new_callable=AsyncMock, return_value=mock_resp):
            result = await llm._non_stream_request("https://dep/v2/completion", {}, {})

        assert result.content.parts[0].text == "Hello!"

    @pytest.mark.asyncio
    async def test_tool_call_response(self):
        llm = _make_llm()
        data = {
            "choices": [
                {
                    "finish_reason": "tool_calls",
                    "message": {
                        "content": "",
                        "tool_calls": [
                            {
                                "id": "call_99",
                                "type": "function",
                                "function": {"name": "get_pods", "arguments": '{"ns":"default"}'},
                            }
                        ],
                    },
                }
            ]
        }
        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = data

        with patch.object(llm._get_http_client(), "post", new_callable=AsyncMock, return_value=mock_resp):
            result = await llm._non_stream_request("https://dep/v2/completion", {}, {})

        fc = next((p.function_call for p in result.content.parts if p.function_call), None)
        assert fc is not None
        assert fc.name == "get_pods"
        assert fc.id == "call_99"
        assert fc.args == {"ns": "default"}

    @pytest.mark.asyncio
    async def test_usage_metadata(self):
        llm = _make_llm()
        data = {
            "choices": [{"finish_reason": "stop", "message": {"content": "ok"}}],
            "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
        }
        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = data

        with patch.object(llm._get_http_client(), "post", new_callable=AsyncMock, return_value=mock_resp):
            result = await llm._non_stream_request("https://dep/v2/completion", {}, {})

        assert result.usage_metadata.prompt_token_count == 10
        assert result.usage_metadata.candidates_token_count == 5
        assert result.usage_metadata.total_token_count == 15


# ---------------------------------------------------------------------------
# _stream_request
# ---------------------------------------------------------------------------


class TestStreamRequest:
    def _orch_chunk(self, choices):
        return {"orchestration_result": {"choices": choices}}

    def _text_delta(self, text):
        return {"delta": {"content": text}}

    def _finish_delta(self, reason):
        return {"delta": {}, "finish_reason": reason}

    @pytest.mark.asyncio
    async def test_text_chunks_and_final_aggregation(self):
        llm = _make_llm()
        payloads = [
            self._orch_chunk([self._text_delta("Hello")]),
            self._orch_chunk([self._text_delta(", world")]),
            self._orch_chunk([self._finish_delta("stop")]),
        ]
        mock_resp = _sse_body(*payloads)

        cm = MagicMock()
        cm.__aenter__ = AsyncMock(return_value=mock_resp)
        cm.__aexit__ = AsyncMock(return_value=False)

        with patch.object(llm._get_http_client(), "stream", return_value=cm):
            responses = [r async for r in llm._stream_request("https://dep/v2/completion", {}, {})]

        partials = [r for r in responses if r.partial]
        assert len(partials) == 2
        assert partials[0].content.parts[0].text == "Hello"
        assert partials[1].content.parts[0].text == ", world"

        final = responses[-1]
        assert not final.partial
        assert final.turn_complete
        assert final.content.parts[0].text == "Hello, world"

    @pytest.mark.asyncio
    async def test_tool_call_assembled_across_chunks(self):
        llm = _make_llm()
        payloads = [
            self._orch_chunk(
                [
                    {
                        "delta": {
                            "tool_calls": [
                                {"index": 0, "id": "call_5", "function": {"name": "list_pods", "arguments": '{"ns":'}}
                            ]
                        }
                    }
                ]
            ),
            self._orch_chunk(
                [
                    {
                        "delta": {"tool_calls": [{"index": 0, "function": {"arguments": '"default"}'}}]},
                        "finish_reason": "tool_calls",
                    }
                ]
            ),
        ]
        mock_resp = _sse_body(*payloads)

        cm = MagicMock()
        cm.__aenter__ = AsyncMock(return_value=mock_resp)
        cm.__aexit__ = AsyncMock(return_value=False)

        with patch.object(llm._get_http_client(), "stream", return_value=cm):
            responses = [r async for r in llm._stream_request("https://dep/v2/completion", {}, {})]

        final = responses[-1]
        fc = next((p.function_call for p in final.content.parts if p.function_call), None)
        assert fc is not None
        assert fc.name == "list_pods"
        assert fc.id == "call_5"
        assert fc.args == {"ns": "default"}

    @pytest.mark.asyncio
    async def test_usage_metadata_in_final_result_envelope(self):
        llm = _make_llm()
        payloads = [
            self._orch_chunk([self._text_delta("hi")]),
            {
                "final_result": {
                    "choices": [self._finish_delta("stop")],
                    "usage": {"prompt_tokens": 8, "completion_tokens": 3, "total_tokens": 11},
                }
            },
        ]
        mock_resp = _sse_body(*payloads)

        cm = MagicMock()
        cm.__aenter__ = AsyncMock(return_value=mock_resp)
        cm.__aexit__ = AsyncMock(return_value=False)

        with patch.object(llm._get_http_client(), "stream", return_value=cm):
            responses = [r async for r in llm._stream_request("https://dep/v2/completion", {}, {})]

        final = responses[-1]
        assert final.usage_metadata is not None
        assert final.usage_metadata.prompt_token_count == 8
        assert final.usage_metadata.candidates_token_count == 3

    @pytest.mark.asyncio
    async def test_error_event_raises(self):
        llm = _make_llm()
        mock_resp = _sse_body({"code": "500", "message": "internal error"})

        cm = MagicMock()
        cm.__aenter__ = AsyncMock(return_value=mock_resp)
        cm.__aexit__ = AsyncMock(return_value=False)

        with patch.object(llm._get_http_client(), "stream", return_value=cm):
            with pytest.raises(RuntimeError, match="internal error"):
                async for _ in llm._stream_request("https://dep/v2/completion", {}, {}):
                    pass

    @pytest.mark.asyncio
    async def test_malformed_lines_skipped(self):
        llm = _make_llm()

        async def _aiter_lines():
            yield "data: not-valid-json"
            yield f"data: {json.dumps(self._orch_chunk([self._text_delta('ok')]))}"
            yield "data: [DONE]"

        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.aiter_lines = _aiter_lines

        cm = MagicMock()
        cm.__aenter__ = AsyncMock(return_value=mock_resp)
        cm.__aexit__ = AsyncMock(return_value=False)

        with patch.object(llm._get_http_client(), "stream", return_value=cm):
            responses = [r async for r in llm._stream_request("https://dep/v2/completion", {}, {})]

        partials = [r for r in responses if r.partial]
        assert len(partials) == 1
        assert partials[0].content.parts[0].text == "ok"


# ---------------------------------------------------------------------------
# generate_content_async — retry on retryable status codes
# ---------------------------------------------------------------------------


class TestGenerateContentAsyncRetry:
    @pytest.mark.asyncio
    async def test_retries_on_401(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        ok_resp = MagicMock()
        ok_resp.raise_for_status = MagicMock()
        ok_resp.json.return_value = {"choices": [{"finish_reason": "stop", "message": {"content": "retry ok"}}]}

        error_resp = MagicMock()
        error_resp.status_code = 401
        http_error = httpx.HTTPStatusError("401", request=MagicMock(), response=error_resp)

        call_count = 0

        async def mock_non_stream(url, headers, body):
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                raise http_error
            return await _make_real_non_stream(url, headers, body)

        from google.adk.models.llm_response import LlmResponse
        from google.genai import types as gtypes

        async def _make_real_non_stream(url, headers, body):
            return LlmResponse(content=gtypes.Content(role="model", parts=[gtypes.Part.from_text(text="retry ok")]))

        with (
            patch.object(llm, "_resolve_deployment_url", new_callable=AsyncMock, return_value="https://dep/"),
            patch.object(llm, "_get_headers", new_callable=AsyncMock, return_value={}),
            patch.object(llm, "_non_stream_request", side_effect=mock_non_stream),
            patch.object(llm, "_invalidate_token") as mock_inv_tok,
            patch.object(llm, "_invalidate_deployment_url") as mock_inv_dep,
        ):
            responses = [r async for r in llm.generate_content_async(_make_request(), stream=False)]

        assert call_count == 2
        mock_inv_tok.assert_called_once()
        mock_inv_dep.assert_called_once()
        assert responses[-1].content.parts[0].text == "retry ok"

    @pytest.mark.asyncio
    async def test_no_retry_on_400(self, monkeypatch):
        llm = _make_llm()
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_ID", "cid")
        monkeypatch.setenv("SAP_AI_CORE_CLIENT_SECRET", "csecret")

        error_resp = MagicMock()
        error_resp.status_code = 400
        http_error = httpx.HTTPStatusError("400", request=MagicMock(), response=error_resp)

        call_count = 0

        async def mock_non_stream(url, headers, body):
            nonlocal call_count
            call_count += 1
            raise http_error

        with (
            patch.object(llm, "_resolve_deployment_url", new_callable=AsyncMock, return_value="https://dep/"),
            patch.object(llm, "_get_headers", new_callable=AsyncMock, return_value={}),
            patch.object(llm, "_non_stream_request", side_effect=mock_non_stream),
        ):
            responses = [r async for r in llm.generate_content_async(_make_request(), stream=False)]

        assert call_count == 1  # no retry
        assert responses[-1].error_code == "API_ERROR"


# ---------------------------------------------------------------------------
# _create_llm_from_model_config integration
# ---------------------------------------------------------------------------


class TestCreateLlmFromModelConfig:
    def test_returns_kagent_sap_ai_core_llm(self):
        from kagent.adk.types import SAPAICore, _create_llm_from_model_config

        config = SAPAICore(
            type="sap_ai_core",
            model="anthropic--claude-3.5-sonnet",
            base_url="https://api.example.com",
            resource_group="my-group",
            auth_url="https://auth.example.com",
        )
        result = _create_llm_from_model_config(config)
        assert isinstance(result, KAgentSAPAICoreLlm)
        assert result.model == "anthropic--claude-3.5-sonnet"
        assert result.base_url == "https://api.example.com"
        assert result.resource_group == "my-group"
        assert result.auth_url == "https://auth.example.com"

    def test_default_resource_group(self):
        from kagent.adk.types import SAPAICore, _create_llm_from_model_config

        config = SAPAICore(
            type="sap_ai_core",
            model="anthropic--claude-3.5-sonnet",
        )
        result = _create_llm_from_model_config(config)
        assert result.resource_group == "default"
