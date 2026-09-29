"""A2A token usage extension primitives.

Version 1 is emitted whether or not the client activated it. The payload lives
in the metadata of a task's terminal status update under the extension URI.
"""

from __future__ import annotations

from a2a.types import AgentCard
from google.protobuf.json_format import ParseDict

USAGE_EXTENSION_URI = "https://kagent.dev/extensions/usage/v1"


def attach_usage_agent_extension(card: AgentCard) -> AgentCard:
    """Declare the optional extension on a protobuf AgentCard without replacing others."""
    if any(extension.uri == USAGE_EXTENSION_URI for extension in card.capabilities.extensions):
        return card
    ParseDict(
        {"uri": USAGE_EXTENSION_URI, "description": "Task-lifetime token usage", "required": False},
        card.capabilities.extensions.add(),
    )
    return card
