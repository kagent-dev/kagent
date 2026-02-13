# Re-export A2A DataPart metadata constants from upstream google-adk.
# These are the canonical definitions — kagent should not redefine them.
from google.adk.a2a.converters.part_converter import (  # noqa: E402
    A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY,
    A2A_DATA_PART_METADATA_TYPE_CODE_EXECUTION_RESULT,
    A2A_DATA_PART_METADATA_TYPE_EXECUTABLE_CODE,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    A2A_DATA_PART_METADATA_TYPE_KEY,
)

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


# Human-in-the-Loop (HITL) Constants
KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL = "tool_approval"
KAGENT_HITL_DECISION_TYPE_KEY = "decision_type"
KAGENT_HITL_DECISION_TYPE_APPROVE = "approve"
KAGENT_HITL_DECISION_TYPE_DENY = "deny"
KAGENT_HITL_DECISION_TYPE_REJECT = "reject"
KAGENT_HITL_RESUME_KEYWORDS_APPROVE = ["approved", "approve", "proceed", "yes", "continue"]
KAGENT_HITL_RESUME_KEYWORDS_DENY = ["denied", "deny", "reject", "no", "cancel", "stop"]
