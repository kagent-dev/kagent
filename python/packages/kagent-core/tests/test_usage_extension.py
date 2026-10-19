"""Tests for the A2A token usage extension helpers."""

from a2a.types import AgentCard

from kagent.core.a2a import USAGE_EXTENSION_URI, attach_usage_agent_extension


def test_attach_usage_agent_extension_is_optional_and_idempotent():
    card = AgentCard(name="agent")
    attach_usage_agent_extension(card)
    attach_usage_agent_extension(card)

    extensions = [extension for extension in card.capabilities.extensions if extension.uri == USAGE_EXTENSION_URI]
    assert len(extensions) == 1
    assert not extensions[0].required
