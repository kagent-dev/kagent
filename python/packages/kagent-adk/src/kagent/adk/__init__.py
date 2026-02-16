import importlib.metadata
import logging

from ._a2a import KAgentApp
from ._logging import install_mcp_content_type_filter
from .types import AgentConfig

__version__ = importlib.metadata.version("kagent_adk")

__all__ = ["KAgentApp", "AgentConfig"]

# Suppress noisy "Unexpected content type" ERROR messages from the MCP
# streamable-HTTP client.  These are emitted every heartbeat interval
# (typically 30 s) when the MCP Go server responds to ping-responses
# without a Content-Type header.  The errors are harmless -- actual MCP
# tool calls are unaffected -- but they obscure real problems in the logs.
# See https://github.com/kagent-dev/kagent/issues/1200
install_mcp_content_type_filter()
