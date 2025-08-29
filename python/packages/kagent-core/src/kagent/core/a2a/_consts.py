A2A_DATA_PART_METADATA_TYPE_KEY = "type"
A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY = "is_long_running"
A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL = "function_call"
A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE = "function_response"
A2A_DATA_PART_METADATA_TYPE_CODE_EXECUTION_RESULT = "code_execution_result"
A2A_DATA_PART_METADATA_TYPE_EXECUTABLE_CODE = "executable_code"

KAGENT_METADATA_KEY_PREFIX = "kagent_"


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
