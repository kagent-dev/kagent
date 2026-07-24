"""Tests for the Gemini model-level generation config wiring."""

import pytest
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai import types
from pydantic import ValidationError

from kagent.adk.models._gemini import _GeminiGenerationConfigMixin
from kagent.adk.types import Gemini, GeminiVertexAI


class _FakeBaseLlm:
    """Stand-in for the native ADK GeminiLLM: records the request config it
    receives and yields a dummy response, without touching any API."""

    def __init__(self, max_output_tokens=None):
        self.max_output_tokens = max_output_tokens
        self.seen_max_output_tokens = "unset"

    async def generate_content_async(self, llm_request, stream: bool = False):
        self.seen_max_output_tokens = llm_request.config.max_output_tokens
        yield LlmResponse()


class _Model(_GeminiGenerationConfigMixin, _FakeBaseLlm):
    pass


def _request(max_output_tokens=None):
    return LlmRequest(
        model="gemini-2.5-flash",
        config=types.GenerateContentConfig(max_output_tokens=max_output_tokens),
    )


@pytest.mark.asyncio
async def test_applies_max_output_tokens_when_unset():
    model = _Model(max_output_tokens=2048)
    req = _request()
    _ = [r async for r in model.generate_content_async(req, stream=False)]
    assert req.config.max_output_tokens == 2048
    assert model.seen_max_output_tokens == 2048


@pytest.mark.asyncio
async def test_does_not_override_per_request_value():
    model = _Model(max_output_tokens=2048)
    req = _request(max_output_tokens=512)
    _ = [r async for r in model.generate_content_async(req, stream=False)]
    # A value the caller/agent already set must win.
    assert req.config.max_output_tokens == 512
    assert model.seen_max_output_tokens == 512


@pytest.mark.asyncio
async def test_noop_when_model_has_no_cap():
    model = _Model(max_output_tokens=None)
    req = _request()
    _ = [r async for r in model.generate_content_async(req, stream=False)]
    assert req.config.max_output_tokens is None
    assert model.seen_max_output_tokens is None


_GEMINI_TYPES = [(Gemini, "gemini"), (GeminiVertexAI, "gemini_vertex_ai")]


@pytest.mark.parametrize("model_cls,type_name", _GEMINI_TYPES)
@pytest.mark.parametrize("bad_value", [0, -1])
def test_rejects_non_positive_max_output_tokens(model_cls, type_name, bad_value):
    # The translator treats <= 0 as "unset"; reject it at parse time so an
    # invalid config fails fast instead of being silently ignored.
    with pytest.raises(ValidationError):
        model_cls(type=type_name, model="gemini-2.5-flash", max_output_tokens=bad_value)


@pytest.mark.parametrize("model_cls,type_name", _GEMINI_TYPES)
def test_accepts_positive_max_output_tokens(model_cls, type_name):
    model = model_cls(type=type_name, model="gemini-2.5-flash", max_output_tokens=1)
    assert model.max_output_tokens == 1
