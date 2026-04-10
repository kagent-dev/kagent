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
from typing import Any, List, Union

import numpy as np

from kagent.adk.types import EmbeddingConfig

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

    async def _embed_openai(self, texts: List[str]) -> List[List[float]]:
        """Embed using the OpenAI or Azure OpenAI SDK."""
        provider = self.config.provider.lower()

        if provider == "azure_openai":
            from openai import AsyncAzureOpenAI

            api_version = os.environ.get("OPENAI_API_VERSION", "2024-02-15-preview")
            api_base = self.config.base_url or os.environ.get("AZURE_OPENAI_ENDPOINT")
            if not api_base:
                raise ValueError("Azure OpenAI endpoint must be set via base_url or AZURE_OPENAI_ENDPOINT env var")
            client = AsyncAzureOpenAI(api_version=api_version, azure_endpoint=api_base)
        else:
            from openai import AsyncOpenAI

            client = AsyncOpenAI(base_url=self.config.base_url or None)

        response = await client.embeddings.create(
            model=self.config.model,
            input=texts,
            dimensions=self.TARGET_DIMENSION,
        )
        return [item.embedding for item in response.data]

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
