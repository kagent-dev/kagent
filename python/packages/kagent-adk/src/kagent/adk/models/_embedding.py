"""Embedding client for generating vector embeddings using various providers.

This module provides a standalone EmbeddingClient that supports multiple providers:
- openai: OpenAI API embeddings
- azure_openai: Azure OpenAI embeddings
- ollama: Ollama local embeddings
- gemini/vertex_ai: Google Gemini/Vertex AI embeddings
- bedrock: AWS Bedrock Titan Embedding API
"""

import asyncio
import json
import logging
import os
from typing import Any, List, Optional, Union

import httpx
import numpy as np

from kagent.adk._bearer_token import bearer_token
from kagent.adk.models._ssl import create_ssl_context
from kagent.adk.types import EmbeddingConfig

from ._azure import (
    build_azure_openai_client,
    resolve_azure_api_key,
    resolve_azure_openai_config,
    resolve_foundry_config,
)

logger = logging.getLogger(__name__)


class KAgentEmbedding:
    """Client for generating embeddings using provider-specific SDKs.

    This client is standalone and has no dependencies on the memory service.
    It supports multiple embedding providers with dimension enforcement and
    L2 normalization.
    """

    # Target dimension for Kagent memory storage (must match go/adk/pkg/embedding/embedding.go)
    TARGET_DIMENSION = 768

    def __init__(self, config: EmbeddingConfig):
        """Initialize EmbeddingClient.

        Args:
            config: Embedding configuration including model, provider, and base_url
        """
        self.config = config

    async def generate(self, texts: Union[str, List[str]]) -> Union[List[float], List[List[float]]]:
        """Generate embedding vector(s) for the given text(s).

        Args:
            texts: Single string or list of strings to embed.

        Returns:
            Single vector (List[float]) if input is string,
            or List of vectors (List[List[float]]) if input is list.
            Returns empty list on failure.
        """
        if not texts:
            return [] if isinstance(texts, list) else []

        is_batch = isinstance(texts, list)
        text_list = texts if is_batch else [texts]

        if not text_list:
            return [] if is_batch else []

        try:
            raw_embeddings = await self._call_provider(text_list)
        except Exception as e:
            logger.error(
                "Error generating embedding with provider=%s model=%s: %s",
                self.config.provider,
                self.config.model,
                e,
            )
            return [] if is_batch else []

        # Enforce dimension consistency and apply L2 normalization
        embeddings = self._process_embeddings(raw_embeddings)

        if is_batch:
            return embeddings
        return embeddings[0] if embeddings else []

    async def _call_provider(self, texts: List[str]) -> List[List[float]]:
        """Dispatch to the correct provider SDK for embedding generation."""
        provider = self.config.provider.lower()

        if provider in ("openai", "azure_openai"):
            return await self._embed_openai(texts)
        if provider == "foundry":
            return await self._embed_foundry(texts)
        if provider == "ollama":
            return await self._embed_ollama(texts)
        if provider in ("vertex_ai", "gemini"):
            return await self._embed_google(texts)
        if provider == "bedrock":
            return await self._embed_bedrock(texts)

        # Unknown provider - try OpenAI-compatible as a fallback
        logger.warning(
            "Unknown embedding provider '%s'; attempting OpenAI-compatible call.",
            provider,
        )
        return await self._embed_openai(texts)

    def _process_embeddings(self, embeddings: List[List[float]]) -> List[List[float]]:
        """Process embeddings to ensure consistent dimensions and L2 normalization.

        Most Matryoshka Representation Learning embedding models produce embeddings
        that still have meaning when truncated to specific sizes:
        https://huggingface.co/blog/matryoshka

        We must ensure embeddings have consistent dimensions for the vector storage backend.
        """
        processed = []

        for embedding in embeddings:
            dim = len(embedding)
            processed_embedding = embedding

            if dim > self.TARGET_DIMENSION:
                # Truncate to target dimension
                processed_embedding = embedding[: self.TARGET_DIMENSION]
                # Re-normalize after truncation
                processed_embedding = self._normalize_l2(processed_embedding).tolist()
            elif dim < self.TARGET_DIMENSION:
                logger.error(
                    "Embedding dimension %d is smaller than required %d; rejecting embeddings batch",
                    dim,
                    self.TARGET_DIMENSION,
                )
                return []

            processed.append(processed_embedding)

        return processed

    def _normalize_l2(self, x: Union[List[float], np.ndarray]) -> np.ndarray:
        """Apply L2 normalization to a vector or array of vectors."""
        x = np.array(x)
        if x.ndim == 1:
            norm = np.linalg.norm(x)
            if norm == 0:
                return x
            return x / norm
        else:
            norm = np.linalg.norm(x, 2, axis=1, keepdims=True)
            return np.where(norm == 0, x, x / norm)

    def _passthrough_api_key(self) -> Optional[str]:
        """Bearer token to use as the API key when api_key_passthrough is
        enabled, mirroring BaseOpenAI.set_passthrough_key for chat models.
        Azure providers treat a missing token as an error rather than falling
        back to a provider environment key.
        """
        if not self.config.api_key_passthrough:
            return None
        return bearer_token.get()

    def _tls_http_client(self) -> Optional[httpx.AsyncClient]:
        """httpx client honoring ModelConfig TLS settings, or None when unset."""
        if not (self.config.tls_disable_verify or self.config.tls_ca_cert_path or self.config.tls_disable_system_cas):
            return None
        return httpx.AsyncClient(
            verify=create_ssl_context(
                disable_verify=self.config.tls_disable_verify or False,
                ca_cert_path=self.config.tls_ca_cert_path,
                disable_system_cas=self.config.tls_disable_system_cas or False,
            )
        )

    async def _embed_openai(self, texts: List[str]) -> List[List[float]]:
        """Embed using the OpenAI or Azure OpenAI SDK."""
        provider = self.config.provider.lower()
        api_key = self._passthrough_api_key()

        http_client = self._tls_http_client()
        client = None
        try:
            if provider == "azure_openai":
                api_base, api_version = resolve_azure_openai_config(
                    self.config.endpoint or self.config.base_url, self.config.api_version
                )
                api_key = resolve_azure_api_key(
                    api_key,
                    api_key_passthrough=self.config.api_key_passthrough,
                    environment_variable="AZURE_OPENAI_API_KEY",
                )
                client = self._build_azure_client(
                    api_version=api_version,
                    endpoint=api_base,
                    deployment=self.config.deployment,
                    api_key=api_key,
                    http_client=http_client,
                )
            else:
                from openai import AsyncOpenAI

                client_kwargs: dict[str, Any] = {"api_key": api_key}
                if http_client is not None:
                    client_kwargs["http_client"] = http_client
                client = AsyncOpenAI(base_url=self.config.base_url or None, **client_kwargs)

            response = await client.embeddings.create(
                model=self.config.model,
                input=texts,
                dimensions=self.TARGET_DIMENSION,
            )
            return [item.embedding for item in response.data]
        finally:
            if client is not None:
                await client.close()
            elif http_client is not None:
                await http_client.aclose()

    async def _embed_foundry(self, texts: List[str]) -> List[List[float]]:
        """Embed using the Azure AI Foundry OpenAI-compatible surface."""
        endpoint, deployment, api_version = resolve_foundry_config(
            self.config.endpoint, self.config.deployment, self.config.api_version
        )
        api_key = resolve_azure_api_key(
            self._passthrough_api_key(),
            api_key_passthrough=self.config.api_key_passthrough,
            environment_variable="FOUNDRY_API_KEY",
        )

        http_client = self._tls_http_client()
        client = None
        try:
            client = self._build_azure_client(
                api_version=api_version,
                endpoint=endpoint,
                deployment=deployment,
                api_key=api_key,
                http_client=http_client,
            )
            response = await client.embeddings.create(
                model=self.config.model,
                input=texts,
            )
            return [item.embedding for item in response.data]
        finally:
            if client is not None:
                await client.close()
            elif http_client is not None:
                await http_client.aclose()

    def _build_azure_client(
        self,
        *,
        api_version: str,
        endpoint: str,
        deployment: Optional[str],
        api_key: Optional[str],
        http_client: Optional[httpx.AsyncClient] = None,
    ):
        """Build an Azure embeddings client with implicit Workload Identity auth."""
        return build_azure_openai_client(
            api_version=api_version,
            azure_endpoint=endpoint,
            azure_deployment=deployment,
            api_key=api_key,
            api_key_passthrough=self.config.api_key_passthrough,
            default_headers=None,
            http_client=http_client,
            missing_credential_hint=(
                "No Azure credential resolved for embeddings: set an API key, enable "
                "api_key_passthrough, or configure Azure Workload Identity"
            ),
        )

    async def _embed_ollama(self, texts: List[str]) -> List[List[float]]:
        """Embed using the Ollama SDK."""
        import ollama

        host = self.config.base_url or os.environ.get("OLLAMA_API_BASE", "http://localhost:11434")
        client = ollama.AsyncClient(host=host)
        result = await client.embed(model=self.config.model, input=texts)
        # Ollama returns embeddings as a list of lists
        embeddings = result.embeddings
        if embeddings and not isinstance(embeddings[0], list):
            # Single embedding case
            return [embeddings]
        return list(embeddings)

    async def _embed_google(self, texts: List[str]) -> List[List[float]]:
        """Embed using google-genai (Gemini or Vertex AI)."""
        from google import genai
        from google.genai import types as genai_types

        if self.config.provider.lower() == "vertex_ai":
            client = genai.Client(vertexai=True)
        else:
            client = genai.Client()

        # Use asyncio.to_thread since genai may not have async methods
        response = await asyncio.to_thread(
            client.models.embed_content,
            model=self.config.model,
            contents=texts,
            config=genai_types.EmbedContentConfig(output_dimensionality=self.TARGET_DIMENSION),
        )
        return [list(emb.values) for emb in response.embeddings]

    async def _embed_bedrock(
        self,
        texts: List[str],
    ) -> List[List[float]]:
        """Embed using the AWS Bedrock Titan Embedding API via boto3.

        Uses the same credential chain (env vars, IRSA, instance profile) as
        KAgentBedrockLlm.  Each text is embedded individually because the
        Titan Embedding API accepts a single ``inputText`` per invocation.
        """
        import boto3

        region = os.environ.get("AWS_DEFAULT_REGION") or os.environ.get("AWS_REGION") or "us-east-1"
        client = boto3.client("bedrock-runtime", region_name=region)

        async def _invoke_single(text: str) -> List[float]:
            body = json.dumps({"inputText": text})
            response = await asyncio.to_thread(
                client.invoke_model,
                modelId=self.config.model,
                body=body,
                contentType="application/json",
                accept="application/json",
            )
            result = json.loads(response["body"].read())
            return result["embedding"]

        embeddings = await asyncio.gather(*[_invoke_single(t) for t in texts])
        return list(embeddings)
