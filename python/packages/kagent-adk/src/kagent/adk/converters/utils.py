from __future__ import annotations

KAGENT_METADATA_KEY_PREFIX = "kagent_"


def _get_kagent_metadata_key(key: str) -> str:
    """Gets the A2A event metadata key for the given key.

    Args:
      key: The metadata key to prefix.

    Returns:
      The prefixed metadata key.

    Raises:
      ValueError: If key is empty or None.
    """
    if not key:
        raise ValueError("Metadata key cannot be empty or None")
    return f"{KAGENT_METADATA_KEY_PREFIX}{key}"
