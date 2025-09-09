"""Error code to user-friendly message mappings for ADK events.

This module provides mappings from Google GenAI finish reasons to user-friendly
error messages, excluding STOP which is a normal completion reason.
"""

from typing import Dict, Optional

from google.genai import types as genai_types

# Error code to user-friendly message mappings
# Based on Google GenAI types.py FinishReason enum (excluding STOP)
ERROR_CODE_MESSAGES: Dict[str, str] = {
    # Length and token limits
    genai_types.FinishReason.MAX_TOKENS: "Response was truncated due to maximum token limit. Try asking a shorter question or breaking it into parts.",
    # Safety and content filtering
    genai_types.FinishReason.SAFETY: "Response was blocked due to safety concerns. Please rephrase your request to avoid potentially harmful content.",
    genai_types.FinishReason.RECITATION: "Response was blocked due to unauthorized citations. Please rephrase your request.",
    genai_types.FinishReason.BLOCKLIST: "Response was blocked due to restricted terminology. Please rephrase your request using different words.",
    genai_types.FinishReason.PROHIBITED_CONTENT: "Response was blocked due to prohibited content. Please rephrase your request.",
    genai_types.FinishReason.SPII: "Response was blocked due to sensitive personal information concerns. Please avoid including personal details.",
    # Function calling errors
    genai_types.FinishReason.MALFORMED_FUNCTION_CALL: "The agent generated an invalid function call. This may be due to complex input data. Try rephrasing your request or breaking it into simpler steps.",
    # Generic fallback
    genai_types.FinishReason.OTHER: "An unexpected error occurred during processing. Please try again or rephrase your request.",
}

# Normal completion reasons that should not be treated as errors
NORMAL_COMPLETION_REASONS = {
    genai_types.FinishReason.STOP,  # Normal completion
}

# Default error message when no specific mapping exists
DEFAULT_ERROR_MESSAGE = "An error occurred during processing"


def _get_error_message(error_code: Optional[str]) -> str:
    """Get a user-friendly error message for the given error code.

    Args:
        error_code: The error code from the ADK event (e.g., finish_reason)

    Returns:
        User-friendly error message string
    """

    # Return mapped message or default
    return ERROR_CODE_MESSAGES.get(error_code, DEFAULT_ERROR_MESSAGE)


def _is_normal_completion(error_code: Optional[str]) -> bool:
    """Check if the error code represents normal completion rather than an error.

    Args:
        error_code: The error code to check

    Returns:
        True if this is a normal completion reason, False otherwise
    """
    return error_code in NORMAL_COMPLETION_REASONS
