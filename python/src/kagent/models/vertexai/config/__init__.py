from typing import List, Optional

from autogen_ext.models.anthropic.config import AnthropicClientConfigurationConfigModel
from pydantic import BaseModel, Field

from .._gemini_vertexai_client import ModelInfo


class VertexAIClientConfiguration(BaseModel):
    model: str = Field(description="Name of the Vertex AI model to use, e.g., 'gemini-1.5-pro-latest'.")
    credentials: Optional[dict] = Field(default=None, description="Google Cloud credentials file path.")
    project: Optional[str] = Field(default=None, description="Google Cloud Project ID (required for Vertex AI).")
    location: Optional[str] = Field(default=None, description="Google Cloud Project Location (required for Vertex AI).")


class GeminiVertexAIClientConfiguration(VertexAIClientConfiguration):
    temperature: Optional[float] = Field(
        default=None, ge=0.0, le=2.0, description="Controls randomness. Lower for less random, higher for more."
    )
    top_p: Optional[float] = Field(default=None, ge=0.0, le=1.0, description="Nucleus sampling parameter.")
    top_k: Optional[int] = Field(default=None, ge=0, description="Top-k sampling parameter.")
    max_output_tokens: Optional[int] = Field(default=None, ge=1, description="Maximum number of tokens to generate.")
    candidate_count: Optional[int] = Field(default=None, ge=1, description="Number of candidate responses to generate.")
    response_mime_type: Optional[str] = Field(default=None, description="Response MIME type.")
    stop_sequences: Optional[List[str]] = Field(default=None, description="Stop sequences.")

    model_info_override: Optional[ModelInfo] = Field(
        default=None, description="Optional override for model capabilities and information."
    )


class AnthropicVertexAIClientConfiguration(VertexAIClientConfiguration, AnthropicClientConfigurationConfigModel):
    pass


__all__ = ["GeminiVertexAIClientConfiguration", "AnthropicVertexAIClientConfiguration"]
