"""Tests for Vertex AI authentication through the egress gateway."""

import google.auth
import httpx
import httpx2
import pytest
from google.genai import Client, types

from kagent.adk.models._vertex import (
    VERTEX_GATEWAY_TOKEN,
    KAgentClaudeVertexLlm,
    skip_vertex_auth,
    vertex_client_kwargs,
)


@pytest.fixture
def vertex_env(monkeypatch):
    monkeypatch.setenv("GOOGLE_CLOUD_PROJECT", "project")
    monkeypatch.setenv("GOOGLE_CLOUD_LOCATION", "us-east5")

    def no_ambient_credentials(*args, **kwargs):
        raise AssertionError("Application Default Credentials must not be resolved")

    monkeypatch.setattr(google.auth, "default", no_ambient_credentials)
    return monkeypatch


@pytest.mark.parametrize(
    ("value", "want"),
    [("true", True), ("1", True), ("TRUE", True), ("false", False), ("", False), ("yes", False)],
)
def test_skip_vertex_auth(monkeypatch, value, want):
    monkeypatch.setenv("KAGENT_SKIP_VERTEX_AUTH", value)
    assert skip_vertex_auth() is want


def test_vertex_client_kwargs_without_switch(monkeypatch):
    monkeypatch.delenv("KAGENT_SKIP_VERTEX_AUTH", raising=False)
    assert vertex_client_kwargs() is None


def test_gemini_vertex_sends_gateway_placeholder(vertex_env):
    vertex_env.setenv("KAGENT_SKIP_VERTEX_AUTH", "true")
    seen = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(200, json={"candidates": [{"content": {"role": "model", "parts": [{"text": "ok"}]}}]})

    client = Client(
        vertexai=True,
        project="project",
        location="us-east5",
        http_options=types.HttpOptions(client_args={"transport": httpx.MockTransport(handler)}),
        **vertex_client_kwargs(),
    )
    client.models.generate_content(model="gemini-2.5-pro", contents="hi")

    assert len(seen) == 1
    assert seen[0].url.host == "us-east5-aiplatform.googleapis.com"
    assert seen[0].headers["authorization"] == f"Bearer {VERTEX_GATEWAY_TOKEN}"


@pytest.mark.asyncio
async def test_claude_vertex_sends_gateway_placeholder(vertex_env):
    vertex_env.setenv("KAGENT_SKIP_VERTEX_AUTH", "true")
    seen = []

    def handler(request: httpx2.Request) -> httpx2.Response:
        seen.append(request)
        return httpx2.Response(
            200,
            json={
                "id": "msg",
                "type": "message",
                "role": "assistant",
                "model": "claude-sonnet-4-5",
                "content": [{"type": "text", "text": "ok"}],
                "stop_reason": "end_turn",
                "usage": {"input_tokens": 1, "output_tokens": 1},
            },
        )

    model = KAgentClaudeVertexLlm(model="claude-sonnet-4-5@20250929")
    client = (await model._get_anthropic_client()).with_options(
        http_client=httpx2.AsyncClient(transport=httpx2.MockTransport(handler))
    )
    await client.messages.create(
        model="claude-sonnet-4-5@20250929", max_tokens=1, messages=[{"role": "user", "content": "hi"}]
    )

    assert len(seen) == 1
    assert seen[0].url.host == "us-east5-aiplatform.googleapis.com"
    assert seen[0].headers["authorization"] == f"Bearer {VERTEX_GATEWAY_TOKEN}"


@pytest.mark.asyncio
async def test_claude_vertex_keeps_google_auth_without_switch(vertex_env):
    vertex_env.delenv("KAGENT_SKIP_VERTEX_AUTH", raising=False)
    model = KAgentClaudeVertexLlm(model="claude-sonnet-4-5@20250929")
    client = await model._get_anthropic_client()
    assert client.access_token is None
