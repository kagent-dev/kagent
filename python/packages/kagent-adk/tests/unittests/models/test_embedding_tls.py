"""Unit tests for embedding-client TLS configuration.

These tests verify that KAgentEmbedding._embed_openai honours the TLS fields
carried on EmbeddingConfig (upstream issue #1992): the OpenAI / Azure OpenAI
clients must receive an httpx client whose ``verify`` reflects the configured
ModelConfig.spec.tls, and the default (no-TLS) path must be unchanged.
"""

import ssl
from unittest import mock

import pytest

from kagent.adk.models._embedding import KAgentEmbedding
from kagent.adk.types import EmbeddingConfig


@pytest.mark.asyncio
async def test_embed_openai_disable_verify_builds_client_with_verify_false():
    """disable_verify=True → create_ssl_context returns False → httpx verify=False."""
    config = EmbeddingConfig(
        provider="openai",
        model="text-embedding-3-small",
        base_url="https://litellm.internal.corp:8080",
        tls_insecure_skip_verify=True,
    )
    embedding = KAgentEmbedding(config)

    with mock.patch("kagent.adk.models._embedding.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._embedding.httpx.AsyncClient") as mock_httpx:
            with mock.patch("openai.AsyncOpenAI") as mock_openai:
                mock_create_ssl.return_value = False
                mock_client = mock.MagicMock()
                mock_httpx.return_value = mock_client
                mock_openai.return_value.embeddings.create = mock.AsyncMock(
                    return_value=mock.MagicMock(data=[])
                )

                await embedding._embed_openai(["hello"])

                # SSL context built from the embedding TLS config
                mock_create_ssl.assert_called_once_with(
                    disable_verify=True,
                    ca_cert_path=None,
                    disable_system_cas=False,
                )
                # httpx client created with verify reflecting the config
                mock_httpx.assert_called_once_with(verify=False)
                # OpenAI client received the custom http_client
                openai_kwargs = mock_openai.call_args[1]
                assert openai_kwargs["http_client"] is mock_client


@pytest.mark.asyncio
async def test_embed_openai_custom_ca_builds_client_with_ssl_context():
    """A custom CA cert path is threaded into create_ssl_context and httpx verify."""
    config = EmbeddingConfig(
        provider="openai",
        model="text-embedding-3-small",
        tls_ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
        tls_disable_system_cas=True,
    )
    embedding = KAgentEmbedding(config)

    with mock.patch("kagent.adk.models._embedding.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._embedding.httpx.AsyncClient") as mock_httpx:
            with mock.patch("openai.AsyncOpenAI") as mock_openai:
                ssl_context = mock.MagicMock(spec=ssl.SSLContext)
                mock_create_ssl.return_value = ssl_context
                mock_client = mock.MagicMock()
                mock_httpx.return_value = mock_client
                mock_openai.return_value.embeddings.create = mock.AsyncMock(
                    return_value=mock.MagicMock(data=[])
                )

                await embedding._embed_openai(["hello"])

                mock_create_ssl.assert_called_once_with(
                    disable_verify=False,
                    ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
                    disable_system_cas=True,
                )
                mock_httpx.assert_called_once_with(verify=ssl_context)
                openai_kwargs = mock_openai.call_args[1]
                assert openai_kwargs["http_client"] is mock_client


@pytest.mark.asyncio
async def test_embed_openai_no_tls_keeps_default_client():
    """Without any TLS field, no custom http client is built (default path)."""
    config = EmbeddingConfig(
        provider="openai",
        model="text-embedding-3-small",
    )
    embedding = KAgentEmbedding(config)

    with mock.patch("kagent.adk.models._embedding.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._embedding.httpx.AsyncClient") as mock_httpx:
            with mock.patch("openai.AsyncOpenAI") as mock_openai:
                mock_openai.return_value.embeddings.create = mock.AsyncMock(
                    return_value=mock.MagicMock(data=[])
                )

                await embedding._embed_openai(["hello"])

                # No TLS config → no SSL context / httpx client built
                mock_create_ssl.assert_not_called()
                mock_httpx.assert_not_called()
                # OpenAI client built without an explicit http_client
                openai_kwargs = mock_openai.call_args[1]
                assert "http_client" not in openai_kwargs


@pytest.mark.asyncio
async def test_embed_azure_openai_threads_tls_http_client():
    """Azure OpenAI client also receives the TLS-aware http client."""
    config = EmbeddingConfig(
        provider="azure_openai",
        model="text-embedding-3-small",
        base_url="https://my-azure.openai.azure.com",
        tls_insecure_skip_verify=True,
    )
    embedding = KAgentEmbedding(config)

    with mock.patch("kagent.adk.models._embedding.create_ssl_context") as mock_create_ssl:
        with mock.patch("kagent.adk.models._embedding.httpx.AsyncClient") as mock_httpx:
            with mock.patch("openai.AsyncAzureOpenAI") as mock_azure:
                mock_create_ssl.return_value = False
                mock_client = mock.MagicMock()
                mock_httpx.return_value = mock_client
                mock_azure.return_value.embeddings.create = mock.AsyncMock(
                    return_value=mock.MagicMock(data=[])
                )

                await embedding._embed_openai(["hello"])

                mock_create_ssl.assert_called_once_with(
                    disable_verify=True,
                    ca_cert_path=None,
                    disable_system_cas=False,
                )
                azure_kwargs = mock_azure.call_args[1]
                assert azure_kwargs["http_client"] is mock_client
