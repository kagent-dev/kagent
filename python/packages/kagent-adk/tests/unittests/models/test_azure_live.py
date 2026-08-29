"""Live Azure AI Foundry checks. Skipped unless AZURE_LIVE=1 and credentials are set."""

from __future__ import annotations

import os

import pytest
from google.adk.models.llm_request import LlmRequest
from google.genai.types import Content, Part
from openai import DefaultAsyncHttpxClient

from kagent.adk.models import AzureOpenAI
from kagent.adk.types import AgentConfig


def _live_enabled() -> bool:
    return os.getenv("AZURE_LIVE") == "1" and bool(os.getenv("AZURE_OPENAI_API_KEY"))


pytestmark = pytest.mark.skipif(not _live_enabled(), reason="set AZURE_LIVE=1 and AZURE_OPENAI_API_KEY")


def _logging_azure(**kwargs) -> tuple[AzureOpenAI, list[str]]:
    recorded_urls: list[str] = []

    class _LoggingAzureOpenAI(AzureOpenAI):
        def _create_http_client(self):
            async def on_request(request):
                recorded_urls.append(str(request.url))

            return DefaultAsyncHttpxClient(event_hooks={"request": [on_request]})

    return _LoggingAzureOpenAI(**kwargs), recorded_urls


def _endpoint() -> str:
    return os.environ["AZURE_OPENAI_ENDPOINT"].rstrip("/")


def _deployment() -> str:
    return os.getenv("AZURE_OPENAI_DEPLOYMENT", "gpt-4.1")


def _request() -> LlmRequest:
    return LlmRequest(
        model=_deployment(),
        contents=[Content(role="user", parts=[Part.from_text(text="Reply with exactly: pong")])],
    )


@pytest.mark.asyncio
async def test_live_python_agent_azure_responses_hits_foundry_v1_path():
    llm, recorded_urls = _logging_azure(
        model=_deployment(),
        type="azure_openai",
        azure_endpoint=_endpoint(),
        azure_deployment=_deployment(),
        api_version="2024-06-01",
        api_format="responses",
    )
    agent = AgentConfig.model_validate(
        {
            "model": {
                "type": "azure_openai",
                "model": _deployment(),
                "endpoint": _endpoint() + "/",
                "deployment": _deployment(),
                "api_version": "2024-06-01",
                "api_format": "responses",
            },
            "description": "Python Responses live smoke",
            "instruction": "Reply with one short sentence.",
        }
    ).to_agent("python_responses_smoke")
    assert agent.model.api_format == "responses"

    results = [resp async for resp in llm.generate_content_async(_request(), stream=False)]
    text = results[-1].content.parts[0].text.strip().lower()
    print("python responses urls:", recorded_urls)
    print("python responses text:", text)
    assert "pong" in text
    assert any("/openai/v1/responses" in url for url in recorded_urls), recorded_urls
    assert all("api-version=" not in url for url in recorded_urls if "/openai/v1/responses" in url)


@pytest.mark.asyncio
async def test_live_python_agent_azure_chat_completions_hits_foundry_deployment_path():
    llm, recorded_urls = _logging_azure(
        model=_deployment(),
        type="azure_openai",
        azure_endpoint=_endpoint(),
        azure_deployment=_deployment(),
        api_version="2024-06-01",
        api_format="chatCompletions",
    )
    agent = AgentConfig.model_validate(
        {
            "model": {
                "type": "azure_openai",
                "model": _deployment(),
                "endpoint": _endpoint() + "/",
                "deployment": _deployment(),
                "api_version": "2024-06-01",
                "api_format": "chatCompletions",
            },
            "description": "Python Chat Completions live smoke",
            "instruction": "Reply with one short sentence.",
        }
    ).to_agent("python_chatcompletions_smoke")
    assert agent.model.api_format == "chatCompletions"

    results = [resp async for resp in llm.generate_content_async(_request(), stream=False)]
    text = results[-1].content.parts[0].text.strip().lower()
    print("python chatCompletions urls:", recorded_urls)
    print("python chatCompletions text:", text)
    assert "pong" in text
    assert any("/chat/completions" in url for url in recorded_urls), recorded_urls
    assert any("api-version=" in url for url in recorded_urls if "/chat/completions" in url)
