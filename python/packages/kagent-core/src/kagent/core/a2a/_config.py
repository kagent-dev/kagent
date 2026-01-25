"""Configuration utilities for a2a-sdk integration."""

import logging
import os

logger = logging.getLogger(__name__)

A2A_MAX_CONTENT_LENGTH_ENV_VAR = "A2A_MAX_CONTENT_LENGTH"
DEFAULT_A2A_MAX_CONTENT_LENGTH = 10 * 1024 * 1024  # 10MB (a2a-sdk default)


def get_a2a_max_content_length() -> int | None:
    """Get the a2a max content length from environment variable.

    Returns the configured max content length to be passed to
    A2AFastAPIApplication or A2AStarletteApplication constructors.

    Environment variable:
        A2A_MAX_CONTENT_LENGTH: Maximum payload size in bytes.
                                Default: 10485760 (10MB, a2a-sdk default)
                                Example: 52428800 (50MB)
                                Set to "0" or "none" for unlimited.

    Returns:
        The max content length in bytes, or None for unlimited.
    """
    max_content_length_str = os.getenv(A2A_MAX_CONTENT_LENGTH_ENV_VAR)
    if max_content_length_str is None:
        # Return None to use the a2a-sdk default (10MB)
        return None

    # Handle special case for unlimited
    if max_content_length_str.lower() in ("0", "none", "unlimited"):
        logger.info("Set a2a MAX_CONTENT_LENGTH to unlimited")
        return None

    try:
        max_content_length = int(max_content_length_str)
        if max_content_length < 0:
            logger.warning(
                f"Invalid {A2A_MAX_CONTENT_LENGTH_ENV_VAR} value: {max_content_length_str} "
                f"(must be non-negative), using default {DEFAULT_A2A_MAX_CONTENT_LENGTH}"
            )
            return DEFAULT_A2A_MAX_CONTENT_LENGTH
        logger.info(f"Set a2a MAX_CONTENT_LENGTH to {max_content_length} bytes")
        return max_content_length
    except ValueError:
        logger.warning(
            f"Invalid {A2A_MAX_CONTENT_LENGTH_ENV_VAR} value: {max_content_length_str}, "
            f"using default {DEFAULT_A2A_MAX_CONTENT_LENGTH}"
        )
        return DEFAULT_A2A_MAX_CONTENT_LENGTH
