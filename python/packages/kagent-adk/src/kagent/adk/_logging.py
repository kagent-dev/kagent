"""Logging filters for suppressing noisy MCP client messages.

The MCP Python SDK's streamable-HTTP client logs an ERROR every time it
receives a response whose ``Content-Type`` header is empty or unexpected.
In kagent deployments the kagent-tools MCP server (built on ``mcp-go``)
returns ``200 OK`` **without** a ``Content-Type`` header when it handles
heartbeat ping-responses.  Because the heartbeat fires every 30 seconds,
the resulting log spam makes it hard to spot real errors.

This module installs a lightweight ``logging.Filter`` on the
``mcp.client.streamable_http`` logger that downgrades those specific
messages from ERROR to DEBUG, keeping the logs clean while still
preserving the information for anyone who enables DEBUG-level logging.

Reference: https://github.com/kagent-dev/kagent/issues/1200
"""

from __future__ import annotations

import logging

_MCP_STREAMABLE_HTTP_LOGGER = "mcp.client.streamable_http"
_UNEXPECTED_CT_PREFIX = "Unexpected content type"


class _UnexpectedContentTypeFilter(logging.Filter):
    """Downgrade 'Unexpected content type' ERROR messages to DEBUG.

    The filter intercepts log records from the ``mcp.client.streamable_http``
    logger.  If the record is at ERROR level and its message starts with
    ``"Unexpected content type"``, the level is lowered to DEBUG so that the
    message is still emitted when debug logging is active but no longer
    pollutes the default (INFO / WARNING / ERROR) output.

    All other records pass through unchanged.
    """

    def filter(self, record: logging.LogRecord) -> bool:
        if (
            record.levelno == logging.ERROR
            and isinstance(record.msg, str)
            and record.msg.startswith(_UNEXPECTED_CT_PREFIX)
        ):
            record.levelno = logging.DEBUG
            record.levelname = logging.getLevelName(logging.DEBUG)
        return True


def install_mcp_content_type_filter() -> None:
    """Install the filter on the ``mcp.client.streamable_http`` logger.

    This function is idempotent -- calling it multiple times will not add
    duplicate filters.
    """
    mcp_logger = logging.getLogger(_MCP_STREAMABLE_HTTP_LOGGER)
    # Guard against duplicate installation.
    for existing in mcp_logger.filters:
        if isinstance(existing, _UnexpectedContentTypeFilter):
            return
    mcp_logger.addFilter(_UnexpectedContentTypeFilter())
