from typing import Dict

from autogen_core.models import ModelInfo

from .types import ModelInfoDict

# https://ai.google.dev/gemini-api/docs/models
_MODEL_INFO: ModelInfoDict = {
    "gemini-2.5-flash": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "gemini-2.5-flash",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "gemini-2.5-pro": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "gemini-2.5-pro",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "gemini-2.5-flash-lite-preview-06-17": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "gemini-2.5-flash",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "gemini-2.0-flash": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "gemini-2.0-flash",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "gemini-2.0-flash-lite": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "gemini-2.0-flash",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    # Anthropic
    "claude-sonnet-4@20250514": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-sonnet-4",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "claude-opus-4@20250514": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-opus-4",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "claude-3-7-sonnet@20250219": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-3-7-sonnet",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "claude-3-5-sonnet-v2@20241022": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-3-5-sonnet-v2",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "claude-3-5-haiku@20241022": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-3-5-haiku",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "claude-3-opus@20240229": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-3-opus",
        "structured_output": True,
    },
    "claude-3-haiku@20240307": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-3-haiku",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "claude-3-5-sonnet@20240620": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "claude-3-5-sonnet",
        "structured_output": True,
        "multiple_system_messages": False,
    },
}

# Model token limits (context window size)
_MODEL_TOKEN_LIMITS: Dict[str, int] = {
    "gemini-2.5-flash": 1_048_576,
    "gemini-2.5-pro": 1_048_576,
    "gemini-2.5-flash-lite-preview-06-17": 1_048_576,
    "gemini-2.0-flash": 1_048_576,
    "gemini-2.0-flash-lite": 1_048_576,
    "claude-sonnet-4@20250514": 64_000,
    "claude-opus-4@20250514": 32_000,
    "claude-3-7-sonnet@20250219": 200_000,
    "claude-3-5-sonnet-v2@20241022": 200_000,
    "claude-3-5-haiku@20241022": 200_000,
    "claude-3-opus@20240229": 200_000,
    "claude-3-haiku@20240307": 200_000,
    "claude-3-5-sonnet@20240620": 200_000,
}


def get_info(model: str) -> ModelInfo:
    """Get the model information for a specific model."""
    # Check for exact match first
    if model in _MODEL_INFO:
        return _MODEL_INFO[model]
    raise KeyError(f"Model '{model}' not found in model info")


def get_token_limit(model: str) -> int:
    """Get the token limit for a specific model."""
    # Check for exact match first
    if model in _MODEL_TOKEN_LIMITS:
        return _MODEL_TOKEN_LIMITS[model]
    return 100000