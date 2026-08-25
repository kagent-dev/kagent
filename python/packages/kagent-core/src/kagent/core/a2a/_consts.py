from typing import Any, Optional

from a2a.types import Message
from google.protobuf.json_format import MessageToDict

# A2A DataPart metadata constants.
# These values MUST match the upstream google-adk definitions in
# google.adk.a2a.converters.part_converter. A sync-check test in
# kagent-adk verifies they stay in sync.
A2A_DATA_PART_METADATA_TYPE_KEY = "type"
A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY = "is_long_running"
A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL = "function_call"
A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE = "function_response"
A2A_DATA_PART_METADATA_TYPE_CODE_EXECUTION_RESULT = "code_execution_result"
A2A_DATA_PART_METADATA_TYPE_EXECUTABLE_CODE = "executable_code"

KAGENT_METADATA_KEY_PREFIX = "kagent_"
ADK_METADATA_KEY_PREFIX = "adk_"


def get_kagent_metadata_key(key: str) -> str:
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


def read_message_metadata(message: Optional[Message]) -> dict[str, Any]:
    """Return a Message's protobuf ``Struct`` metadata as a plain dict.

    Args:
      message: The A2A message to read (may be ``None``).

    Returns:
      The decoded metadata, or an empty dict when the message carries none.
    """
    if message is None or not message.HasField("metadata"):
        return {}
    return MessageToDict(message.metadata)


def read_metadata_value(metadata: dict | None, key: str, default=None):
    """Read a metadata value, checking ``adk_<key>`` first then ``kagent_<key>``.

    This allows interoperability with upstream ADK (which uses the ``adk_``
    prefix) while preserving backward-compatibility with kagent's own
    ``kagent_`` prefix.

    Args:
      metadata: The metadata dict to look up (may be ``None``).
      key: The unprefixed key name (e.g. ``"type"``).
      default: Value returned when the key is not found under either prefix.

    Returns:
      The value found under ``adk_<key>`` or ``kagent_<key>``, or *default*.

    Raises:
      ValueError: If *key* is empty or ``None``.
    """
    if not key:
        raise ValueError("Metadata key cannot be empty or None")
    if not metadata:
        return default
    adk_key = f"{ADK_METADATA_KEY_PREFIX}{key}"
    if adk_key in metadata:
        return metadata[adk_key]
    kagent_key = f"{KAGENT_METADATA_KEY_PREFIX}{key}"
    if kagent_key in metadata:
        return metadata[kagent_key]
    return default
