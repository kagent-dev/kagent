"""Tests for embedding generation in KagentMemoryService without litellm."""

from unittest import mock

import numpy as np
import pytest

from kagent.adk._memory_service import KagentMemoryService
from kagent.adk.types import EmbeddingConfig


def make_service(provider: str, model: str, base_url: str | None = None) -> KagentMemoryService:
    return KagentMemoryService(
        agent_name="test-agent",
        http_client=mock.AsyncMock(),
        embedding_config=EmbeddingConfig(provider=provider, model=model, base_url=base_url),
    )


def make_openai_embedding_response(vectors: list[list[float]]):
    """Build a mock that looks like openai.types.CreateEmbeddingResponse."""
    items = []
    for vec in vectors:
        item = mock.MagicMock()
        item.embedding = vec
        items.append(item)
    response = mock.MagicMock()
    response.data = items
    return response


class TestEmbeddingDispatch:
    @pytest.mark.asyncio
    async def test_no_config_returns_empty(self):
        svc = KagentMemoryService(agent_name="x", http_client=mock.AsyncMock(), embedding_config=None)
        result = await svc._generate_embedding_async("hello")
        assert result == []

    @pytest.mark.asyncio
    async def test_empty_model_returns_empty(self):
        svc = make_service(provider="openai", model="")
        result = await svc._generate_embedding_async("hello")
        assert result == []

    @pytest.mark.asyncio
    async def test_openai_embed(self):
        svc = make_service(provider="openai", model="text-embedding-3-small")
        vec = [0.1] * 768
        mock_response = make_openai_embedding_response([vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await svc._generate_embedding_async("hello world")
        assert result == vec

    @pytest.mark.asyncio
    async def test_azure_openai_uses_azure_client(self):
        svc = make_service(
            provider="azure_openai", model="text-embedding-ada-002", base_url="https://myazure.openai.azure.com"
        )
        vec = [0.5] * 768
        mock_response = make_openai_embedding_response([vec])
        with (
            mock.patch.dict(
                "os.environ",
                {"OPENAI_API_VERSION": "2024-02-01", "AZURE_OPENAI_ENDPOINT": "https://myazure.openai.azure.com"},
            ),
            mock.patch("openai.AsyncAzureOpenAI") as mock_cls,
        ):
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await svc._generate_embedding_async("hello")
        assert result == vec
        assert mock_cls.called

    @pytest.mark.asyncio
    async def test_ollama_embed(self):
        svc = make_service(provider="ollama", model="nomic-embed-text")
        vecs = [[0.1] * 768]
        mock_result = mock.MagicMock()
        mock_result.embeddings = vecs
        mock_client = mock.AsyncMock()
        mock_client.embed = mock.AsyncMock(return_value=mock_result)

        with mock.patch("ollama.AsyncClient") as mock_cls:
            mock_cls.return_value = mock_client
            result = await svc._generate_embedding_async("test text")

        assert result == vecs[0]
        mock_client.embed.assert_called_once_with(model="nomic-embed-text", input=["test text"])

    @pytest.mark.asyncio
    async def test_ollama_uses_api_base_url(self):
        svc = make_service(provider="ollama", model="nomic-embed-text", base_url="http://custom-ollama:11434")
        mock_result = mock.MagicMock()
        mock_result.embeddings = [[0.0] * 768]
        mock_client = mock.AsyncMock()
        mock_client.embed = mock.AsyncMock(return_value=mock_result)

        with mock.patch("ollama.AsyncClient") as mock_cls:
            mock_cls.return_value = mock_client
            await svc._generate_embedding_async("hello")
            mock_cls.assert_called_once_with(host="http://custom-ollama:11434")

    @pytest.mark.asyncio
    async def test_embedding_truncated_and_normalized(self):
        svc = make_service(provider="openai", model="text-embedding-3-large")
        long_vec = [1.0] * 1000
        mock_response = make_openai_embedding_response([long_vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await svc._generate_embedding_async("test")
        assert len(result) == 768
        assert abs(np.linalg.norm(result) - 1.0) < 1e-5

    @pytest.mark.asyncio
    async def test_unknown_provider_falls_back_to_openai(self):
        svc = make_service(provider="custom_provider", model="my-model")
        vec = [0.1] * 768
        mock_response = make_openai_embedding_response([vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await svc._generate_embedding_async("test")
        assert result == vec

    @pytest.mark.asyncio
    async def test_provider_error_returns_empty_list(self):
        svc = make_service(provider="openai", model="text-embedding-3-small")
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(side_effect=Exception("API error"))
            mock_cls.return_value = instance
            result = await svc._generate_embedding_async("test")
        assert result == []

    @pytest.mark.asyncio
    async def test_embedding_shorter_than_768_rejected(self):
        svc = make_service(provider="openai", model="text-embedding-3-small")
        short_vec = [0.1] * 64
        mock_response = make_openai_embedding_response([short_vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await svc._generate_embedding_async("test")
        assert result == []

    @pytest.mark.asyncio
    async def test_bedrock_embed(self):
        svc = make_service(provider="bedrock", model="amazon.titan-embed-text-v1")
        vec = [0.1] * 1536
        mock_response = mock.MagicMock()
        mock_response.__getitem__ = lambda self, key: {"body": mock.MagicMock(read=lambda: b'{"embedding": ' + str(vec).encode() + b"}")}[key]
        mock_client = mock.MagicMock()
        mock_client.invoke_model = mock.MagicMock(return_value={"body": mock.MagicMock(read=lambda: b'{"embedding": ' + str(vec).encode() + b"}")})

        with mock.patch("boto3.client", return_value=mock_client):
            result = await svc._generate_embedding_async("hello world")
        assert len(result) == 768
        mock_client.invoke_model.assert_called_once()

    @pytest.mark.asyncio
    async def test_bedrock_embed_uses_region_from_env(self):
        svc = make_service(provider="bedrock", model="amazon.titan-embed-text-v1")
        vec = [0.5] * 1536
        mock_client = mock.MagicMock()
        mock_client.invoke_model = mock.MagicMock(return_value={"body": mock.MagicMock(read=lambda: b'{"embedding": ' + str(vec).encode() + b"}")})

        with (
            mock.patch.dict("os.environ", {"AWS_REGION": "eu-west-1"}),
            mock.patch("boto3.client", return_value=mock_client) as mock_boto,
        ):
            await svc._generate_embedding_async("test")
        mock_boto.assert_called_once_with("bedrock-runtime", region_name="eu-west-1")

    @pytest.mark.asyncio
    async def test_bedrock_embed_error_returns_empty(self):
        svc = make_service(provider="bedrock", model="amazon.titan-embed-text-v1")
        mock_client = mock.MagicMock()
        mock_client.invoke_model = mock.MagicMock(side_effect=Exception("Bedrock API error"))

        with mock.patch("boto3.client", return_value=mock_client):
            result = await svc._generate_embedding_async("test")
        assert result == []
