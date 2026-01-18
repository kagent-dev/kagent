"""Tests for a2a configuration utilities."""

import os
from unittest.mock import patch

from kagent.core.a2a._config import (
    DEFAULT_A2A_MAX_CONTENT_LENGTH,
    get_a2a_max_content_length,
)


def test_get_a2a_max_content_length_with_env_var():
    """Test that setting A2A_MAX_CONTENT_LENGTH env var returns the configured value."""
    with patch.dict(os.environ, {"A2A_MAX_CONTENT_LENGTH": "52428800"}):
        result = get_a2a_max_content_length()
        assert result == 52428800  # 50MB


def test_get_a2a_max_content_length_without_env_var():
    """Test that without env var, None is returned (use a2a-sdk default)."""
    with patch.dict(os.environ, {}, clear=True):
        # Ensure env var is not set
        os.environ.pop("A2A_MAX_CONTENT_LENGTH", None)
        result = get_a2a_max_content_length()
        assert result is None


def test_get_a2a_max_content_length_with_zero():
    """Test that setting env var to '0' returns None (unlimited)."""
    with patch.dict(os.environ, {"A2A_MAX_CONTENT_LENGTH": "0"}):
        result = get_a2a_max_content_length()
        assert result is None


def test_get_a2a_max_content_length_with_none_string():
    """Test that setting env var to 'none' returns None (unlimited)."""
    with patch.dict(os.environ, {"A2A_MAX_CONTENT_LENGTH": "none"}):
        result = get_a2a_max_content_length()
        assert result is None


def test_get_a2a_max_content_length_with_unlimited_string():
    """Test that setting env var to 'unlimited' returns None."""
    with patch.dict(os.environ, {"A2A_MAX_CONTENT_LENGTH": "unlimited"}):
        result = get_a2a_max_content_length()
        assert result is None


def test_get_a2a_max_content_length_with_invalid_value(caplog):
    """Test that invalid env var value logs a warning and returns default."""
    with patch.dict(os.environ, {"A2A_MAX_CONTENT_LENGTH": "not_a_number"}):
        result = get_a2a_max_content_length()
        assert result == DEFAULT_A2A_MAX_CONTENT_LENGTH
        assert "Invalid A2A_MAX_CONTENT_LENGTH value" in caplog.text
        assert "not_a_number" in caplog.text


def test_get_a2a_max_content_length_with_negative_value(caplog):
    """Test that negative env var value logs a warning and returns default."""
    with patch.dict(os.environ, {"A2A_MAX_CONTENT_LENGTH": "-1"}):
        result = get_a2a_max_content_length()
        assert result == DEFAULT_A2A_MAX_CONTENT_LENGTH
        assert "Invalid A2A_MAX_CONTENT_LENGTH value" in caplog.text
        assert "-1" in caplog.text
        assert "must be non-negative" in caplog.text


def test_get_a2a_max_content_length_exported():
    """Test that get_a2a_max_content_length is exported from kagent.core.a2a."""
    from kagent.core.a2a import get_a2a_max_content_length as exported_func

    assert callable(exported_func)


def test_default_max_content_length_is_10mb():
    """Test that the default constant is 10MB (matching a2a-sdk default)."""
    assert DEFAULT_A2A_MAX_CONTENT_LENGTH == 10 * 1024 * 1024  # 10MB
