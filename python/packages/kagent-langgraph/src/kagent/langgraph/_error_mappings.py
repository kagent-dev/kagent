"""Error code to user-friendly message mappings for LangGraph events."""

# Map common exception types to user-friendly messages
ERROR_TYPE_MESSAGES = {
    "TimeoutError": "Request timed out. Please try again or simplify your request.",
    "ValidationError": "Invalid input provided. Please check your request format.",
    "RateLimitError": "Rate limit exceeded. Please wait a moment and try again.",
    "AuthenticationError": "Authentication failed. Please check your credentials.",
    "PermissionError": "Permission denied. You don't have access to this resource.",
    "ValueError": "Invalid value provided. Please check your input.",
    "KeyError": "Required field missing. Please provide all required information.",
    "ConnectionError": "Connection failed. Please check your network and try again.",
    "HTTPError": "HTTP request failed. The external service may be unavailable.",
    "APIError": "API request failed. Please try again later.",
    "BadRequestError": "Invalid request. Please check your input and try again.",
    "NotFoundError": "Resource not found. Please check the identifier and try again.",
    "RuntimeError": "An unexpected error occurred during execution.",
}

DEFAULT_ERROR_MESSAGE = "An error occurred during processing. Please try again or rephrase your request."


def get_user_friendly_error_message(exception: Exception) -> str:
    """Get a user-friendly error message for the given exception.

    Args:
        exception: The exception that was raised

    Returns:
        A user-friendly error message string
    """
    error_type = type(exception).__name__
    return ERROR_TYPE_MESSAGES.get(error_type, DEFAULT_ERROR_MESSAGE)


def get_error_metadata(exception: Exception) -> dict[str, str]:
    """Get metadata dict with error details.

    Args:
        exception: The exception that was raised

    Returns:
        Dict with error_type and error_detail
    """
    return {
        "error_type": type(exception).__name__,
        "error_detail": str(exception),
    }
