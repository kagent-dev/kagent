"""Exercise credential propagation at the SDK HTTP boundary, without provider calls."""

import asyncio
import io
import json
from unittest import mock

import httpx
import pytest
from botocore.awsrequest import AWSResponse
from google.adk.models.llm_request import LlmRequest
from google.genai import types

from kagent.adk._bearer_token import bearer_token
from kagent.adk._invocation_auth import InvocationCredentialError, policy_url, request_headers
from kagent.adk.models._bedrock import KAgentBedrockLlm, _get_bedrock_client
from kagent.adk.models._embedding import KAgentEmbedding
from kagent.adk.types import EmbeddingConfig


class RawResponse(io.BytesIO):
    def stream(self, amt=None, decode_content=False):
        yield self.read()


@pytest.fixture(autouse=True)
def credentials(monkeypatch):
    source = request_headers.set({})
    policy = policy_url.set(None)
    token = bearer_token.set(None)
    monkeypatch.setenv("AWS_EC2_METADATA_DISABLED", "true")
    monkeypatch.setenv("AWS_DEFAULT_REGION", "us-east-1")
    for name in ("AWS_BEARER_TOKEN_BEDROCK", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"):
        monkeypatch.delenv(name, raising=False)
    yield
    request_headers.reset(source)
    policy_url.reset(policy)
    bearer_token.reset(token)


def bedrock_response(request, payload, status=200):
    return AWSResponse(request.url, status, {}, RawResponse(json.dumps(payload).encode()))


def chat_request():
    return LlmRequest(
        contents=[types.Content(role="user", parts=[types.Part.from_text(text="hello")])],
    )


@pytest.mark.asyncio
async def test_bedrock_chat_uses_current_caller_and_requires_credentials(monkeypatch):
    sent = []

    def send(session, request):
        sent.append(httpx.Headers(request.headers)["authorization"])
        return bedrock_response(
            request,
            {"output": {"message": {"role": "assistant", "content": [{"text": "ok"}]}}, "stopReason": "end_turn"},
        )

    monkeypatch.setattr("botocore.httpsession.URLLib3Session.send", send)
    llm = KAgentBedrockLlm(
        model="amazon.nova-micro-v1:0",
        extra_headers={"Authorization": "Bearer kagent-credential-injected"},
        passthrough_headers=[{"name": "Authorization", "prefix": "Bearer "}],
    )
    try:
        for caller in ("human", "machine"):
            bearer_token.set(caller)
            responses = [response async for response in llm.generate_content_async(chat_request())]
            assert responses[0].content.parts[0].text == "ok"
        bearer_token.set(None)
        responses = [response async for response in llm.generate_content_async(chat_request())]
        assert responses[0].error_code == "API_ERROR"
        assert sent == ["Bearer human", "Bearer machine"]
    finally:
        llm._client.close()


@pytest.mark.asyncio
async def test_bedrock_streaming_preserves_invocation_context():
    llm = KAgentBedrockLlm(model="amazon.nova-micro-v1:0")
    client = mock.Mock()

    def stream(**kwargs):
        assert bearer_token.get() == "machine"
        assert request_headers.get() == {"x-request-id": "current-invocation"}
        return {"stream": [{"contentBlockDelta": {"delta": {"text": "ok"}}}]}

    client.converse_stream.side_effect = stream
    bearer_token.set("machine")
    request_headers.set({"x-request-id": "current-invocation"})
    with mock.patch("kagent.adk.models._bedrock._get_bedrock_client", return_value=client):
        responses = [response async for response in llm.generate_content_async(chat_request(), stream=True)]
    assert responses[-1].content.parts[0].text == "ok"


@pytest.mark.asyncio
@pytest.mark.parametrize("revoked", [False, True])
async def test_bedrock_retries_resolve_the_live_grant(monkeypatch, revoked):
    sent = []

    def broker(request):
        if sent and revoked:
            return httpx.Response(401)
        if request.url.path == "/runtime/egress":
            return httpx.Response(
                200,
                json=[
                    {
                        "origin": "https://bedrock-runtime.us-east-1.amazonaws.com:443",
                        "passthrough": [{"name": "Authorization", "prefix": "Bearer "}],
                    }
                ],
            )
        raise AssertionError("only policy may be fetched")

    original_client = httpx.AsyncClient
    monkeypatch.setattr(
        httpx, "AsyncClient", lambda **kwargs: original_client(transport=httpx.MockTransport(broker), **kwargs)
    )

    def send(session, request):
        sent.append(httpx.Headers(request.headers)["authorization"])
        return bedrock_response(request, {} if len(sent) > 1 else {"message": "retry"}, 200 if len(sent) > 1 else 503)

    monkeypatch.setattr("botocore.httpsession.URLLib3Session.send", send)
    monkeypatch.setattr("botocore.endpoint.time.sleep", lambda delay: None)
    policy_url.set("http://controller/runtime/egress")
    bearer_token.set("original")
    client = _get_bedrock_client(passthrough_headers=[{"name": "Authorization", "prefix": "Bearer "}])
    try:
        call = asyncio.to_thread(client.converse, modelId="amazon.nova-micro-v1:0", messages=[])
        if revoked:
            with pytest.raises(InvocationCredentialError):
                await call
            assert sent == ["Bearer original"]
        else:
            await call
            assert sent == ["Bearer original", "Bearer original"]
    finally:
        client.close()


def test_bedrock_keeps_native_aws_auth_without_managed_authorization(monkeypatch):
    sent = []

    def send(session, request):
        sent.append(httpx.Headers(request.headers))
        return bedrock_response(request, {})

    monkeypatch.setenv("AWS_ACCESS_KEY_ID", "test-access-key")
    monkeypatch.setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
    monkeypatch.setattr("botocore.httpsession.URLLib3Session.send", send)
    client = _get_bedrock_client(extra_headers={"X-Trace": "trace"})
    try:
        client.converse(modelId="amazon.nova-micro-v1:0", messages=[])
    finally:
        client.close()
    assert sent[0]["authorization"].startswith("AWS4-HMAC-SHA256 ")
    assert sent[0]["x-trace"] == "trace"


@pytest.mark.asyncio
@pytest.mark.parametrize("provider", ["ollama", "gemini", "bedrock"])
@pytest.mark.parametrize("authorized", [True, False])
async def test_embeddings_apply_live_credentials_and_close_transports(monkeypatch, provider, authorized):
    origin = {
        "ollama": "https://ollama.example.com:443",
        "gemini": "https://generativelanguage.googleapis.com:443",
        "bedrock": "https://bedrock-runtime.us-east-1.amazonaws.com:443",
    }[provider]
    header = "x-goog-api-key" if provider == "gemini" else "Authorization"
    sent = []
    clients = []
    raw_responses = []

    def handle(request):
        if request.url.host == "controller":
            if not authorized:
                return httpx.Response(401)
            if request.url.path == "/runtime/egress":
                return httpx.Response(200, json=[{"origin": origin, "passthrough": [{"name": header}]}])
            raise AssertionError("only policy may be fetched")
        sent.append(request)
        payload = {"embeddings": [{"values": [0.1] * 768}]} if provider == "gemini" else {"embeddings": [[0.1] * 768]}
        return httpx.Response(200, json=payload)

    original_client = httpx.AsyncClient

    def http_client(**kwargs):
        client = original_client(transport=httpx.MockTransport(handle), **kwargs)
        clients.append(client)
        return client

    monkeypatch.setattr(httpx, "AsyncClient", http_client)

    def send(session, request):
        sent.append(httpx.Request(request.method, request.url, headers=request.headers))
        response = bedrock_response(request, {"embedding": [0.1] * 768})
        raw_responses.append(response.raw)
        return response

    monkeypatch.setattr("botocore.httpsession.URLLib3Session.send", send)
    monkeypatch.setenv("GOOGLE_API_KEY", "kagent-credential-injected")
    policy_url.set("http://controller/runtime/egress")
    bearer_token.set("machine-token")
    config = EmbeddingConfig(
        provider=provider,
        model="gemini-embedding-001" if provider == "gemini" else "embedding-model",
        headers={header: "kagent-credential-injected"},
        passthrough_headers=[{"name": header}],
    )
    if provider == "ollama":
        config.base_url = "https://ollama.example.com"
    embedding = KAgentEmbedding(config)
    if authorized:
        assert await embedding.generate("hello") == [0.1] * 768
        assert len(sent) == 1
        assert sent[0].headers[header] == "machine-token"
    else:
        with pytest.raises(InvocationCredentialError):
            await embedding.generate("hello")
        assert not sent
    assert clients and all(client.is_closed for client in clients)
    assert all(response.closed for response in raw_responses)
