"""Tests for EmbeddingClient without litellm."""

from unittest import mock

import numpy as np
import pytest

from kagent.adk.models import KAgentEmbedding
from kagent.adk.types import EmbeddingConfig


def make_client(provider: str, model: str, base_url: str | None = None) -> KAgentEmbedding:
    return KAgentEmbedding(
        config=EmbeddingConfig(provider=provider, model=model, base_url=base_url),
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


class TestEmbeddingClient:
    @pytest.mark.asyncio
    async def test_generate_single_text(self):
        client = make_client(provider="openai", model="text-embedding-3-small")
        vec = [0.1] * 768
        mock_response = make_openai_embedding_response([vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await client.generate("hello world")
        assert result == vec

    @pytest.mark.asyncio
    async def test_generate_batch_texts(self):
        client = make_client(provider="openai", model="text-embedding-3-small")
        vecs = [[0.1] * 768, [0.2] * 768]
        mock_response = make_openai_embedding_response(vecs)
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await client.generate(["hello", "world"])
        assert len(result) == 2
        assert result[0] == vecs[0]
        assert result[1] == vecs[1]

    @pytest.mark.asyncio
    async def test_empty_input_returns_empty(self):
        client = make_client(provider="openai", model="text-embedding-3-small")
        result = await client.generate("")
        assert result == []

    @pytest.mark.asyncio
    async def test_empty_list_input_returns_empty(self):
        client = make_client(provider="openai", model="text-embedding-3-small")
        result = await client.generate([])
        assert result == []


class TestEmbeddingDispatch:
    @pytest.mark.asyncio
    async def test_openai_embed(self):
        client = make_client(provider="openai", model="text-embedding-3-small")
        vec = [0.1] * 768
        mock_response = make_openai_embedding_response([vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await client.generate("hello world")
        assert result == vec

    @pytest.mark.asyncio
    async def test_azure_openai_uses_azure_client(self):
        client = make_client(
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
            result = await client.generate("hello")
        assert result == vec
        assert mock_cls.called

    @pytest.mark.asyncio
    async def test_ollama_embed(self):
        client = make_client(provider="ollama", model="nomic-embed-text")
        vecs = [[0.1] * 768]
        mock_result = mock.MagicMock()
        mock_result.embeddings = vecs
        mock_client = mock.AsyncMock()
        mock_client.embed = mock.AsyncMock(return_value=mock_result)

        with mock.patch("ollama.AsyncClient") as mock_cls:
            mock_cls.return_value = mock_client
            result = await client.generate("test text")

        assert result == vecs[0]
        mock_client.embed.assert_called_once_with(model="nomic-embed-text", input=["test text"])

    @pytest.mark.asyncio
    async def test_ollama_uses_api_base_url(self):
        client = make_client(provider="ollama", model="nomic-embed-text", base_url="http://custom-ollama:11434")
        mock_result = mock.MagicMock()
        mock_result.embeddings = [[0.0] * 768]
        mock_client = mock.AsyncMock()
        mock_client.embed = mock.AsyncMock(return_value=mock_result)

        with mock.patch("ollama.AsyncClient") as mock_cls:
            mock_cls.return_value = mock_client
            await client.generate("hello")
            mock_cls.assert_called_once_with(host="http://custom-ollama:11434")

    @pytest.mark.asyncio
    async def test_embedding_truncated_and_normalized(self):
        client = make_client(provider="openai", model="text-embedding-3-large")
        long_vec = [1.0] * 1000
        mock_response = make_openai_embedding_response([long_vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await client.generate("test")
        assert len(result) == 768
        assert abs(np.linalg.norm(result) - 1.0) < 1e-5

    @pytest.mark.asyncio
    async def test_unknown_provider_falls_back_to_openai(self):
        client = make_client(provider="custom_provider", model="my-model")
        vec = [0.1] * 768
        mock_response = make_openai_embedding_response([vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await client.generate("test")
        assert result == vec

    @pytest.mark.asyncio
    async def test_provider_error_returns_empty_list(self):
        client = make_client(provider="openai", model="text-embedding-3-small")
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(side_effect=Exception("API error"))
            mock_cls.return_value = instance
            result = await client.generate("test")
        assert result == []

    @pytest.mark.asyncio
    async def test_embedding_shorter_than_768_rejected(self):
        client = make_client(provider="openai", model="text-embedding-3-small")
        short_vec = [0.1] * 64
        mock_response = make_openai_embedding_response([short_vec])
        with mock.patch("openai.AsyncOpenAI") as mock_cls:
            instance = mock.AsyncMock()
            instance.embeddings.create = mock.AsyncMock(return_value=mock_response)
            mock_cls.return_value = instance
            result = await client.generate("test")
        assert result == []


class TestEmbeddingNormalization:
    def test_normalize_l2_unit_vector(self):
        client = make_client(provider="openai", model="test")
        vec = [3.0, 4.0]  # Norm should be 5
        result = client._normalize_l2(vec)
        expected_norm = 1.0
        assert abs(np.linalg.norm(result) - expected_norm) < 1e-6

    def test_normalize_l2_zero_vector(self):
        client = make_client(provider="openai", model="test")
        vec = [0.0, 0.0, 0.0]
        result = client._normalize_l2(vec)
        assert np.allclose(result, vec)

    def test_normalize_l2_batch(self):
        client = make_client(provider="openai", model="test")
        vecs = [[3.0, 4.0], [1.0, 0.0]]
        result = client._normalize_l2(vecs)
        for i in range(len(vecs)):
            assert abs(np.linalg.norm(result[i]) - 1.0) < 1e-6
