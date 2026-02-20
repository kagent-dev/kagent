"""Tests for the MCP content-type logging filter."""

import logging

import pytest

from kagent.adk._logging import (
    _MCP_STREAMABLE_HTTP_LOGGER,
    _UnexpectedContentTypeFilter,
    install_mcp_content_type_filter,
)


@pytest.fixture(autouse=True)
def _cleanup_filters():
    """Remove test filters after each test to avoid cross-test contamination."""
    mcp_logger = logging.getLogger(_MCP_STREAMABLE_HTTP_LOGGER)
    original_filters = list(mcp_logger.filters)
    yield
    mcp_logger.filters = original_filters


class TestUnexpectedContentTypeFilter:
    """Tests for _UnexpectedContentTypeFilter."""

    def test_downgrades_unexpected_content_type_error_to_debug(self):
        filt = _UnexpectedContentTypeFilter()
        record = logging.LogRecord(
            name=_MCP_STREAMABLE_HTTP_LOGGER,
            level=logging.ERROR,
            pathname="",
            lineno=0,
            msg="Unexpected content type: ",
            args=(),
            exc_info=None,
        )
        assert filt.filter(record) is True
        assert record.levelno == logging.DEBUG
        assert record.levelname == "DEBUG"

    def test_downgrades_unexpected_content_type_with_value(self):
        filt = _UnexpectedContentTypeFilter()
        record = logging.LogRecord(
            name=_MCP_STREAMABLE_HTTP_LOGGER,
            level=logging.ERROR,
            pathname="",
            lineno=0,
            msg="Unexpected content type: text/html",
            args=(),
            exc_info=None,
        )
        assert filt.filter(record) is True
        assert record.levelno == logging.DEBUG

    def test_does_not_affect_other_error_messages(self):
        filt = _UnexpectedContentTypeFilter()
        record = logging.LogRecord(
            name=_MCP_STREAMABLE_HTTP_LOGGER,
            level=logging.ERROR,
            pathname="",
            lineno=0,
            msg="Some other error",
            args=(),
            exc_info=None,
        )
        assert filt.filter(record) is True
        assert record.levelno == logging.ERROR
        assert record.levelname == "ERROR"

    def test_does_not_affect_non_error_levels(self):
        filt = _UnexpectedContentTypeFilter()
        record = logging.LogRecord(
            name=_MCP_STREAMABLE_HTTP_LOGGER,
            level=logging.WARNING,
            pathname="",
            lineno=0,
            msg="Unexpected content type: ",
            args=(),
            exc_info=None,
        )
        assert filt.filter(record) is True
        assert record.levelno == logging.WARNING

    def test_always_returns_true(self):
        """The filter should never suppress records, only downgrade the level."""
        filt = _UnexpectedContentTypeFilter()

        for level in (logging.DEBUG, logging.INFO, logging.WARNING, logging.ERROR, logging.CRITICAL):
            record = logging.LogRecord(
                name=_MCP_STREAMABLE_HTTP_LOGGER,
                level=level,
                pathname="",
                lineno=0,
                msg="Unexpected content type: ",
                args=(),
                exc_info=None,
            )
            assert filt.filter(record) is True


class TestInstallMcpContentTypeFilter:
    """Tests for install_mcp_content_type_filter."""

    def test_installs_filter_on_mcp_logger(self):
        mcp_logger = logging.getLogger(_MCP_STREAMABLE_HTTP_LOGGER)
        # Remove any pre-existing filters (e.g., from kagent.adk.__init__)
        mcp_logger.filters = [f for f in mcp_logger.filters if not isinstance(f, _UnexpectedContentTypeFilter)]
        assert len([f for f in mcp_logger.filters if isinstance(f, _UnexpectedContentTypeFilter)]) == 0

        install_mcp_content_type_filter()

        count = len([f for f in mcp_logger.filters if isinstance(f, _UnexpectedContentTypeFilter)])
        assert count == 1

    def test_idempotent(self):
        """Calling install_mcp_content_type_filter multiple times should not
        add duplicate filters."""
        install_mcp_content_type_filter()
        install_mcp_content_type_filter()
        install_mcp_content_type_filter()

        mcp_logger = logging.getLogger(_MCP_STREAMABLE_HTTP_LOGGER)
        count = len([f for f in mcp_logger.filters if isinstance(f, _UnexpectedContentTypeFilter)])
        assert count == 1

    def test_integration_error_becomes_debug(self, caplog):
        """End-to-end: an ERROR log from the MCP logger with the matching
        message should appear at DEBUG level."""
        install_mcp_content_type_filter()
        mcp_logger = logging.getLogger(_MCP_STREAMABLE_HTTP_LOGGER)

        with caplog.at_level(logging.DEBUG, logger=_MCP_STREAMABLE_HTTP_LOGGER):
            mcp_logger.error("Unexpected content type: ")

        # The record should be present but at DEBUG level.
        matching = [
            r
            for r in caplog.records
            if r.name == _MCP_STREAMABLE_HTTP_LOGGER and "Unexpected content type" in r.message
        ]
        assert len(matching) == 1
        assert matching[0].levelno == logging.DEBUG
