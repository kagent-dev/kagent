# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import logging
import ssl
import tempfile
from pathlib import Path
from unittest import mock

import pytest

from kagent.adk.models._ssl import create_ssl_context


def test_ssl_context_verification_disabled():
    """Test SSL context with verification disabled returns False."""
    result = create_ssl_context(
        verify_disabled=True,
        ca_cert_path=None,
        use_system_cas=True,
    )
    assert result is False


def test_ssl_context_with_system_cas_only():
    """Test SSL context with system CAs only."""
    ctx = create_ssl_context(
        verify_disabled=False,
        ca_cert_path=None,
        use_system_cas=True,
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

            ctx = create_ssl_context(
                verify_disabled=False,
                ca_cert_path=cert_path,
                use_system_cas=False,
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

            ctx = create_ssl_context(
                verify_disabled=False,
                ca_cert_path=cert_path,
                use_system_cas=True,
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
            verify_disabled=False,
            ca_cert_path="/nonexistent/path/to/cert.crt",
            use_system_cas=False,
        )


def test_ssl_context_disabled_logs_warning(caplog):
    """Test that disabling SSL verification logs a prominent warning."""
    with caplog.at_level(logging.WARNING):
        result = create_ssl_context(
            verify_disabled=True,
            ca_cert_path=None,
            use_system_cas=True,
        )
        assert result is False
        assert "SSL VERIFICATION DISABLED" in caplog.text
        assert "development/testing" in caplog.text.lower()


def test_ssl_context_with_custom_ca_path_none_uses_system():
    """Test SSL context with ca_cert_path=None uses only system CAs."""
    ctx = create_ssl_context(
        verify_disabled=False,
        ca_cert_path=None,
        use_system_cas=True,
    )
    assert isinstance(ctx, ssl.SSLContext)
    # Default context should have system CAs loaded
    assert ctx.check_hostname is True
    assert ctx.verify_mode == ssl.CERT_REQUIRED
