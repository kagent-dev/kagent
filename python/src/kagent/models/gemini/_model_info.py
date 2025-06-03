from typing import Dict

from autogen_core.models import ModelInfo

# https://ai.google.dev/gemini-api/docs/models
_MODEL_INFO: Dict[str, ModelInfo] = {
    "gemini-2.5-flash-preview-05-20": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "gemini-2.0-flash",
        "structured_output": True,
        "multiple_system_messages": False,
    },
    "gemini-2.5-pro-preview-05-06": {
        "vision": False,
        "function_calling": True,
        "json_output": True,
        "family": "gemini-2.5-pro",
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
}

# Model token limits (context window size)
_MODEL_TOKEN_LIMITS: Dict[str, int] = {
    "gemini-2.5-flash-preview-05-20": 1_048_576,
    "gemini-2.5-pro-preview-05-06": 1_048_576,
    "gemini-2.0-flash": 1_048_576,
    "gemini-2.0-flash-lite": 1_048_576,
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
