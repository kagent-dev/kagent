"""Unit tests for SSL/TLS context creation.

These tests verify the create_ssl_context() function behavior in isolation:
- Function logic and return values
- Configuration options
- Error handling
- Logging behavior
"""

import logging
import ssl
import tempfile
from pathlib import Path
from unittest import mock

import pytest

from kagent.adk.models._ssl import create_ssl_context


def test_ssl_context_verification_disabled():
    """Test SSL context with verification disabled returns False."""
    ssl_context = create_ssl_context(
        disable_verify=True,
        ca_cert_path=None,
        disable_system_cas=False,
    )
    assert ssl_context is False


def test_ssl_context_with_system_cas_only():
    """Test SSL context with system CAs only."""
    ctx = create_ssl_context(
        disable_verify=False,
        ca_cert_path=None,
        disable_system_cas=False,
    )
    assert isinstance(ctx, ssl.SSLContext)
    assert ctx.check_hostname is True
    assert ctx.verify_mode == ssl.CERT_REQUIRED


def test_ssl_context_with_custom_ca_only():
    """Test SSL context with custom CA certificate only (no system CAs)."""
    # Create a temporary file to simulate certificate
    with tempfile.NamedTemporaryFile(mode="w", suffix=".crt", delete=False) as f:
        f.write("dummy cert content")
        cert_path = f.name

    try:
        # Mock the SSLContext to avoid actual certificate validation
        with mock.patch("ssl.SSLContext") as mock_ssl_context:
            mock_ctx = mock.MagicMock()
            mock_ssl_context.return_value = mock_ctx

            _ = create_ssl_context(
                disable_verify=False,
                ca_cert_path=cert_path,
                disable_system_cas=True,
            )

            # Verify SSLContext was created with correct protocol
            mock_ssl_context.assert_called_once_with(ssl.PROTOCOL_TLS_CLIENT)

            # Verify context attributes were set
            assert mock_ctx.check_hostname is True
            assert mock_ctx.verify_mode == ssl.CERT_REQUIRED

            # Verify certificate was loaded
            mock_ctx.load_verify_locations.assert_called_once()
            assert str(cert_path) in str(mock_ctx.load_verify_locations.call_args)
    finally:
        Path(cert_path).unlink()


def test_ssl_context_with_system_and_custom_ca():
    """Test SSL context with both system and custom CA certificates."""
    # Create a temporary file to simulate certificate
    with tempfile.NamedTemporaryFile(mode="w", suffix=".crt", delete=False) as f:
        f.write("dummy cert content")
        cert_path = f.name

    try:
        # Mock ssl.create_default_context to avoid loading system CAs
        with mock.patch("ssl.create_default_context") as mock_create_default:
            mock_ctx = mock.MagicMock()
            mock_create_default.return_value = mock_ctx

            _ = create_ssl_context(
                disable_verify=False,
                ca_cert_path=cert_path,
                disable_system_cas=False,
            )

            # Verify default context was created (includes system CAs)
            mock_create_default.assert_called_once()

            # Verify certificate was loaded in addition to system CAs
            mock_ctx.load_verify_locations.assert_called_once()
            assert str(cert_path) in str(mock_ctx.load_verify_locations.call_args)
    finally:
        Path(cert_path).unlink()


def test_ssl_context_certificate_file_not_found():
    """Test SSL context with non-existent certificate file."""
    with pytest.raises(FileNotFoundError):
        create_ssl_context(
            disable_verify=False,
            ca_cert_path="/nonexistent/path/to/cert.crt",
            disable_system_cas=True,
        )


def test_ssl_context_disabled_logs_warning(caplog):
    """Test that disabling SSL verification logs a prominent warning."""
    with caplog.at_level(logging.WARNING):
        ssl_context = create_ssl_context(
            disable_verify=True,
            ca_cert_path=None,
            disable_system_cas=False,
        )
        assert ssl_context is False
        assert "SSL VERIFICATION DISABLED" in caplog.text
        assert "development/testing" in caplog.text.lower()
