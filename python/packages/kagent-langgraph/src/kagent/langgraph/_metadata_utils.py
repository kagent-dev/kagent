"""Metadata utilities for rich event metadata."""

import logging
from typing import Any

from kagent.core.a2a import get_kagent_metadata_key

logger = logging.getLogger(__name__)


def serialize_metadata_value(value: Any) -> str:
    """Safely serializes metadata values to string format.

    Args:
        value: The value to serialize

    Returns:
        String representation of the value
    """
    if hasattr(value, "model_dump"):
        try:
            return str(value.model_dump(exclude_none=True, by_alias=True))
        except Exception as e:
            logger.warning(f"Failed to serialize metadata value: {e}")
            return str(value)
    return str(value)


def get_rich_event_metadata(
    app_name: str,
    session_id: str,
    user_id: str | None = None,
    invocation_id: str | None = None,
    extra_fields: dict[str, Any] | None = None,
) -> dict[str, str]:
    """Get rich metadata for A2A events.

    Args:
        app_name: Application name
        session_id: Session/context ID
        user_id: Optional user identifier
        invocation_id: Optional invocation/request identifier
        extra_fields: Optional additional metadata fields

    Returns:
        Dict with namespaced metadata keys
    """
    metadata = {
        get_kagent_metadata_key("app_name"): app_name,
        get_kagent_metadata_key("session_id"): session_id,
    }

    # Add optional core fields
    if user_id:
        metadata[get_kagent_metadata_key("user_id")] = user_id
    if invocation_id:
        metadata[get_kagent_metadata_key("invocation_id")] = invocation_id

    # Add extra fields if provided
    if extra_fields:
        for key, value in extra_fields.items():
            if value is not None:
                metadata[get_kagent_metadata_key(key)] = serialize_metadata_value(value)

    return metadata
