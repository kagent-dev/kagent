"""Tests for LLMHeaderPassthroughPlugin."""

from types import SimpleNamespace
from unittest import mock

import pytest
from google.adk.models.llm_request import LlmRequest
from google.genai import types as genai_types

from kagent.adk._llm_header_passthrough_plugin import (
    LLMHeaderPassthroughPlugin,
    _resolve_passthrough_headers,
)


def _callback_context(model, headers):
    ctx = mock.MagicMock()
    ctx.state = {"headers": headers}
    ctx._invocation_context.agent.model = model
    return ctx


class TestResolvePassthroughHeaders:
    def test_case_insensitive_match_preserves_configured_casing(self):
        resolved = _resolve_passthrough_headers(
            ["X-Guardrail-Token"], {"x-guardrail-token": "caller-token", "x-ignored": "nope"}
        )
        assert resolved == {"X-Guardrail-Token": "caller-token"}

    def test_restricted_names_are_dropped(self):
        resolved = _resolve_passthrough_headers(
            ["Authorization", "Proxy-Authorization", "Cookie", "Connection", "x-ok"],
            {
                "authorization": "Bearer caller",
                "proxy-authorization": "Basic caller",
                "cookie": "session=abc",
                "connection": "close",
                "x-ok": "yes",
            },
        )
        assert resolved == {"x-ok": "yes"}

    def test_missing_or_empty_values_are_omitted(self):
        resolved = _resolve_passthrough_headers(["x-missing", "x-empty"], {"x-empty": ""})
        assert resolved == {}


class TestLLMHeaderPassthroughPlugin:
    @pytest.mark.asyncio
    async def test_noop_when_model_has_no_passthrough_headers(self):
        model = SimpleNamespace(passthrough_headers=None)
        llm_request = LlmRequest(model="gpt-4o", contents=[])
        llm_request.config = None
        ctx = _callback_context(model, {"x-guardrail-token": "caller-token"})

        result = await LLMHeaderPassthroughPlugin().before_model_callback(callback_context=ctx, llm_request=llm_request)

        assert result is None
        assert llm_request.config is None

    @pytest.mark.asyncio
    async def test_noop_when_no_configured_header_in_request(self):
        model = SimpleNamespace(passthrough_headers=["x-guardrail-token"])
        llm_request = LlmRequest(model="gpt-4o", contents=[])
        llm_request.config = None
        ctx = _callback_context(model, {"x-other": "value"})

        await LLMHeaderPassthroughPlugin().before_model_callback(callback_context=ctx, llm_request=llm_request)

        assert llm_request.config is None

    @pytest.mark.asyncio
    async def test_resolved_headers_merge_into_http_options(self):
        model = SimpleNamespace(passthrough_headers=["x-guardrail-token"])
        llm_request = LlmRequest(model="gpt-4o", contents=[])
        llm_request.config = genai_types.GenerateContentConfig(
            http_options=genai_types.HttpOptions(headers={"x-existing": "keep"})
        )
        ctx = _callback_context(model, {"x-guardrail-token": "caller-token"})

        await LLMHeaderPassthroughPlugin().before_model_callback(callback_context=ctx, llm_request=llm_request)

        assert llm_request.config.http_options.headers == {
            "x-existing": "keep",
            "x-guardrail-token": "caller-token",
        }

    @pytest.mark.asyncio
    async def test_creates_config_and_http_options_when_absent(self):
        model = SimpleNamespace(passthrough_headers=["x-guardrail-token"])
        llm_request = LlmRequest(model="gpt-4o", contents=[])
        llm_request.config = None
        ctx = _callback_context(model, {"x-guardrail-token": "caller-token"})

        await LLMHeaderPassthroughPlugin().before_model_callback(callback_context=ctx, llm_request=llm_request)

        assert llm_request.config.http_options.headers == {"x-guardrail-token": "caller-token"}

    @pytest.mark.asyncio
    async def test_model_with_setter_receives_headers_directly(self):
        received: dict[str, str] = {}

        class SetterModel:
            passthrough_headers = ["x-guardrail-token"]

            def set_passthrough_headers(self, headers):
                received.update(headers)

        llm_request = LlmRequest(model="claude-sonnet-4-5", contents=[])
        llm_request.config = None
        ctx = _callback_context(SetterModel(), {"x-guardrail-token": "caller-token"})

        await LLMHeaderPassthroughPlugin().before_model_callback(callback_context=ctx, llm_request=llm_request)

        assert received == {"x-guardrail-token": "caller-token"}
        assert llm_request.config is None

    @pytest.mark.asyncio
    async def test_setter_model_receives_empty_dict_to_clear_previous_caller(self):
        """A caller sending no configured headers must clear, not inherit, the previous caller's values."""
        calls: list[dict[str, str]] = []

        class SetterModel:
            passthrough_headers = ["x-guardrail-token"]

            def set_passthrough_headers(self, headers):
                calls.append(dict(headers))

        llm_request = LlmRequest(model="claude-sonnet-4-5", contents=[])
        llm_request.config = None
        ctx = _callback_context(SetterModel(), {"x-other": "value"})

        await LLMHeaderPassthroughPlugin().before_model_callback(callback_context=ctx, llm_request=llm_request)

        assert calls == [{}]
