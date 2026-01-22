"""Unit tests for SSL/TLS context creation and client certificate loading.

These tests verify the SSL/TLS utility functions behavior in isolation:
- create_ssl_context() function:
  - Function logic and return values
  - Configuration options
  - Error handling
  - Logging behavior
- load_client_certificate() function:
  - Successful certificate and key loading
  - Error handling for missing files
  - Validation of file contents
  - Logging behavior
"""

import logging
import ssl
import tempfile
from pathlib import Path
from unittest import mock

import pytest

from kagent.adk.models._ssl import create_ssl_context, load_client_certificate


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


# Tests for load_client_certificate function


def test_load_client_certificate_success():
    """Test successful loading of client certificate and key."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"
        key_file = cert_dir / "tls.key"

        # Create dummy certificate and key files
        cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy cert content\n-----END CERTIFICATE-----")
        key_file.write_text("-----BEGIN PRIVATE KEY-----\ndummy key content\n-----END PRIVATE KEY-----")

        # Load the certificate
        cert_path, key_path, ca_path = load_client_certificate(temp_dir)

        # Verify return values
        assert cert_path == str(cert_file)
        assert key_path == str(key_file)
        assert ca_path is None  # No ca.crt in this test
        assert Path(cert_path).exists()
        assert Path(key_path).exists()


def test_load_client_certificate_directory_not_found():
    """Test FileNotFoundError when certificate directory does not exist."""
    with pytest.raises(FileNotFoundError) as exc_info:
        load_client_certificate("/nonexistent/directory/path")

    error_message = str(exc_info.value)
    assert "Client certificate directory not found" in error_message
    assert "kubectl get secret" in error_message


def test_load_client_certificate_cert_file_not_found():
    """Test FileNotFoundError when tls.crt file is missing."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        key_file = cert_dir / "tls.key"

        # Create only the key file, not the certificate file
        key_file.write_text("-----BEGIN PRIVATE KEY-----\ndummy key content\n-----END PRIVATE KEY-----")

        with pytest.raises(FileNotFoundError) as exc_info:
            load_client_certificate(temp_dir)

        error_message = str(exc_info.value)
        assert "Client certificate file not found" in error_message
        assert "tls.crt" in error_message
        assert "Secret contains tls.crt key" in error_message


def test_load_client_certificate_key_file_not_found():
    """Test FileNotFoundError when tls.key file is missing."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"

        # Create only the certificate file, not the key file
        cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy cert content\n-----END CERTIFICATE-----")

        with pytest.raises(FileNotFoundError) as exc_info:
            load_client_certificate(temp_dir)

        error_message = str(exc_info.value)
        assert "Client private key file not found" in error_message
        assert "tls.key" in error_message
        assert "Secret contains tls.key key" in error_message


def test_load_client_certificate_empty_key_file():
    """Test ValueError when key file is empty."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"
        key_file = cert_dir / "tls.key"

        # Create certificate file and empty key file
        cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy cert content\n-----END CERTIFICATE-----")
        key_file.write_text("")  # Empty key file

        with pytest.raises(ValueError) as exc_info:
            load_client_certificate(temp_dir)

        error_message = str(exc_info.value)
        assert "Client private key file is empty" in error_message


def test_load_client_certificate_invalid_cert_format_logs_warning(caplog):
    """Test that invalid certificate format logs warning but still loads."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"
        key_file = cert_dir / "tls.key"

        # Create invalid certificate (not valid PEM format) but valid key
        cert_file.write_text("invalid cert content")
        key_file.write_text("-----BEGIN PRIVATE KEY-----\ndummy key content\n-----END PRIVATE KEY-----")

        with caplog.at_level(logging.WARNING):
            cert_path, key_path, ca_path = load_client_certificate(temp_dir)
            assert ca_path is None  # No ca.crt in this test

        # Should still return paths even with invalid cert format
        assert cert_path == str(cert_file)
        assert key_path == str(key_file)

        # Should log a warning about certificate validation
        assert "Could not validate client certificate format" in caplog.text or "validate" in caplog.text.lower()


def test_load_client_certificate_logs_info(caplog):
    """Test that successful loading logs info messages."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"
        key_file = cert_dir / "tls.key"

        # Create valid certificate and key files
        cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy cert content\n-----END CERTIFICATE-----")
        key_file.write_text("-----BEGIN PRIVATE KEY-----\ndummy key content\n-----END PRIVATE KEY-----")

        cert_path, key_path, ca_path = load_client_certificate(temp_dir)

        # Verify certificate and key paths are returned correctly
        assert cert_path == str(cert_file)
        assert key_path == str(key_file)
        assert ca_path is None  # No ca.crt in this test


def test_load_client_certificate_with_ca_logs_info(caplog):
    """Test that loading with CA certificate logs appropriate messages."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"
        key_file = cert_dir / "tls.key"
        ca_cert_file = cert_dir / "ca.crt"

        # Create valid certificate, key, and CA certificate files
        cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy cert content\n-----END CERTIFICATE-----")
        key_file.write_text("-----BEGIN PRIVATE KEY-----\ndummy key content\n-----END PRIVATE KEY-----")
        ca_cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy ca cert content\n-----END CERTIFICATE-----")

        cert_path, key_path, ca_path = load_client_certificate(temp_dir)

        # Verify certificate, key, and CA paths are returned correctly
        assert cert_path == str(cert_file)
        assert key_path == str(key_file)
        assert ca_path == str(ca_cert_file)


def test_load_client_certificate_with_ca_cert():
    """Test loading client certificate with optional CA certificate."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"
        key_file = cert_dir / "tls.key"
        ca_cert_file = cert_dir / "ca.crt"

        # Create dummy certificate, key, and CA certificate files
        cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy cert content\n-----END CERTIFICATE-----")
        key_file.write_text("-----BEGIN PRIVATE KEY-----\ndummy key content\n-----END PRIVATE KEY-----")
        ca_cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy ca cert content\n-----END CERTIFICATE-----")

        # Load the certificate
        cert_path, key_path, ca_path = load_client_certificate(temp_dir)

        # Verify return values
        assert cert_path == str(cert_file)
        assert key_path == str(key_file)
        assert ca_path == str(ca_cert_file)  # ca.crt should be found
        assert Path(cert_path).exists()
        assert Path(key_path).exists()
        assert Path(ca_path).exists()


def test_load_client_certificate_key_file_read_error():
    """Test ValueError when key file cannot be read."""
    with tempfile.TemporaryDirectory() as temp_dir:
        cert_dir = Path(temp_dir)
        cert_file = cert_dir / "tls.crt"
        key_file = cert_dir / "tls.key"

        # Create certificate file
        cert_file.write_text("-----BEGIN CERTIFICATE-----\ndummy cert content\n-----END CERTIFICATE-----")

        # Create key file with restricted permissions (if possible) or mock read error
        key_file.write_text("-----BEGIN PRIVATE KEY-----\ndummy key content\n-----END PRIVATE KEY-----")

        # Mock open to raise an IOError when reading the key file
        with mock.patch("builtins.open", side_effect=IOError("Permission denied")):
            with pytest.raises(ValueError) as exc_info:
                load_client_certificate(temp_dir)

            error_message = str(exc_info.value)
            assert "Failed to read client private key" in error_message
            assert "PEM format" in error_message
