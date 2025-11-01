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

"""Integration tests for TLS/SSL configuration end-to-end workflows.

These tests verify the complete TLS configuration flow:
Secret → Volume Mount → Environment Variables → Certificate Loading → SSL Context
"""

import logging
import os
import ssl
import tempfile
from pathlib import Path
from unittest import mock

import pytest

from kagent.adk.models._openai import OpenAI
from kagent.adk.models._ssl import create_ssl_context, get_ssl_troubleshooting_message, validate_certificate


# ============================================================================
# Test Fixtures
# ============================================================================


@pytest.fixture
def temp_cert_file():
    """Create a temporary certificate file for testing."""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".crt", delete=False) as f:
        # Write a minimal PEM certificate structure
        f.write(
            """-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKoJxVlQ9/7GMA0GCSqGSIb3DQEBCwUAMBExDzANBgNVBAMMBnRl
c3RDQTAeFw0yNTAxMDEwMDAwMDBaFw0yNjAxMDEwMDAwMDBaMBExDzANBgNVBAMM
BnRlc3RDQTCBnzANBgkqhkiG9w0BAQEFAAOBjQAwgYkCgYEAwmOKnW5IkKqCQbpc
Y0JqB2lMfN0LxBBxVlGJKJbJXZcJlZXbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfb
fbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfb
fbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbfbCAwEAATANBgkqhkiG
9w0BAQsFAAOBgQC5G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7
G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7G7==
-----END CERTIFICATE-----
"""
        )
        cert_path = f.name

    yield cert_path

    # Cleanup
    Path(cert_path).unlink(missing_ok=True)


@pytest.fixture
def mock_env_vars(temp_cert_file):
    """Mock environment variables for TLS configuration."""
    env_vars = {
        "TLS_VERIFY_DISABLED": "false",
        "TLS_CA_CERT_PATH": temp_cert_file,
        "TLS_USE_SYSTEM_CAS": "true",
    }
    with mock.patch.dict(os.environ, env_vars, clear=False):
        yield env_vars


# ============================================================================
# Integration Test 1: End-to-End Environment Variable Flow
# ============================================================================


def test_e2e_environment_variable_to_ssl_context(mock_env_vars):
    """Test end-to-end flow: Environment variables → SSL context creation."""
    # Simulate the flow in a Kubernetes pod:
    # 1. Controller sets environment variables from ModelConfig TLS config
    # 2. Python runtime reads environment variables
    # 3. SSL context is created

    verify_disabled = os.getenv("TLS_VERIFY_DISABLED", "false").lower() == "true"
    ca_cert_path = os.getenv("TLS_CA_CERT_PATH")
    use_system_cas = os.getenv("TLS_USE_SYSTEM_CAS", "true").lower() == "true"

    # Verify environment variables are parsed correctly
    assert verify_disabled is False
    assert ca_cert_path == mock_env_vars["TLS_CA_CERT_PATH"]
    assert use_system_cas is True

    # Create SSL context
    with mock.patch("ssl.create_default_context") as mock_default_ctx:
        mock_ctx = mock.MagicMock()
        mock_default_ctx.return_value = mock_ctx

        ctx = create_ssl_context(
            verify_disabled=verify_disabled,
            ca_cert_path=ca_cert_path,
            use_system_cas=use_system_cas,
        )

        # Verify SSL context was created with correct settings
        mock_default_ctx.assert_called_once()
        # Verify certificate was loaded (via validate_certificate and load_verify_locations)
        mock_ctx.load_verify_locations.assert_called_once()
        assert ctx is mock_ctx


# ============================================================================
# Integration Test 2: Certificate Validation Flow
# ============================================================================


def test_e2e_certificate_validation_flow(temp_cert_file, caplog):
    """Test certificate validation is called during SSL context creation."""
    with caplog.at_level(logging.INFO):
        with mock.patch("ssl.create_default_context") as mock_default_ctx:
            with mock.patch.object(ssl.SSLContext, "load_verify_locations"):
                mock_ctx = mock.MagicMock()
                mock_default_ctx.return_value = mock_ctx

                ctx = create_ssl_context(
                    verify_disabled=False,
                    ca_cert_path=temp_cert_file,
                    use_system_cas=True,
                )

                # Verify certificate validation logs appear
                # Note: validate_certificate may log warnings for test cert,
                # but should not block SSL context creation
                assert ctx is mock_ctx
                assert "TLS Mode" in caplog.text


# ============================================================================
# Integration Test 3: Backward Compatibility (No TLS Config)
# ============================================================================


def test_e2e_backward_compatibility_no_tls_config():
    """Test that agents work without TLS configuration (backward compatibility)."""
    # Simulate agent starting without TLS environment variables
    with mock.patch.dict(
        os.environ,
        {},
        clear=False,
    ):
        # Remove TLS env vars if they exist
        for key in ["TLS_VERIFY_DISABLED", "TLS_CA_CERT_PATH", "TLS_USE_SYSTEM_CAS"]:
            os.environ.pop(key, None)

        # Create OpenAI client without TLS config
        openai_llm = OpenAI(model="gpt-3.5-turbo", type="openai", api_key="test-key")

        # Verify client is created successfully
        assert openai_llm is not None
        assert openai_llm.model == "gpt-3.5-turbo"


# ============================================================================
# Integration Test 4: Invalid Certificate File Path
# ============================================================================


def test_e2e_invalid_certificate_path():
    """Test error handling when certificate file does not exist."""
    with pytest.raises(FileNotFoundError) as exc_info:
        create_ssl_context(
            verify_disabled=False,
            ca_cert_path="/nonexistent/path/ca.crt",
            use_system_cas=True,
        )

    # Verify error message includes troubleshooting guidance
    assert "CA certificate file not found" in str(exc_info.value)
    assert "kubectl get secret" in str(exc_info.value)


# ============================================================================
# Integration Test 5: All Three TLS Modes
# ============================================================================


@pytest.mark.parametrize(
    "verify_disabled,ca_cert_path,use_system_cas,expected_mode",
    [
        (True, None, True, "disabled"),
        (False, None, True, "system_only"),
        (False, "fake_path", False, "custom_only"),
        (False, "fake_path", True, "custom_and_system"),
    ],
    ids=["disabled", "system_only", "custom_only", "custom_and_system"],
)
def test_e2e_all_tls_modes(verify_disabled, ca_cert_path, use_system_cas, expected_mode, caplog, temp_cert_file):
    """Test all three TLS configuration modes work correctly."""
    with caplog.at_level(logging.INFO):
        if ca_cert_path == "fake_path":
            ca_cert_path = temp_cert_file

        if expected_mode == "disabled":
            ctx = create_ssl_context(
                verify_disabled=verify_disabled,
                ca_cert_path=ca_cert_path,
                use_system_cas=use_system_cas,
            )
            assert ctx is False
            assert "TLS Mode: Disabled" in caplog.text
        else:
            with mock.patch("ssl.create_default_context") as mock_default_ctx:
                with mock.patch.object(ssl.SSLContext, "load_verify_locations"):
                    mock_ctx = mock.MagicMock()
                    if use_system_cas:
                        mock_default_ctx.return_value = mock_ctx
                    else:
                        # For custom_only mode, mock SSLContext constructor
                        with mock.patch("ssl.SSLContext") as mock_ssl_ctx:
                            mock_ssl_ctx.return_value = mock_ctx
                            ctx = create_ssl_context(
                                verify_disabled=verify_disabled,
                                ca_cert_path=ca_cert_path,
                                use_system_cas=use_system_cas,
                            )
                            assert ctx is mock_ctx
                        return

                    ctx = create_ssl_context(
                        verify_disabled=verify_disabled,
                        ca_cert_path=ca_cert_path,
                        use_system_cas=use_system_cas,
                    )
                    assert ctx is mock_ctx

            # Verify appropriate mode is logged
            if expected_mode == "system_only":
                assert "System CAs only" in caplog.text
            elif expected_mode == "custom_and_system":
                assert "Custom CA + System CAs" in caplog.text


# ============================================================================
# Integration Test 6: SSL Error Troubleshooting Message
# ============================================================================


def test_e2e_ssl_error_troubleshooting_message(temp_cert_file):
    """Test that SSL errors generate helpful troubleshooting messages."""
    error = ssl.SSLError("certificate verify failed")

    message = get_ssl_troubleshooting_message(
        error=error,
        ca_cert_path=temp_cert_file,
        server_url="litellm.internal.corp:8080",
    )

    # Verify troubleshooting message contains actionable guidance
    assert "SSL/TLS Connection Error" in message
    assert "kubectl exec" in message
    assert "openssl x509" in message
    assert "openssl s_client" in message
    assert temp_cert_file in message
    assert "litellm.internal.corp:8080" in message
    assert "https://docs.kagent.dev/troubleshooting/ssl-errors" in message


# ============================================================================
# Integration Test 7: OpenAI Client with Environment Variables
# ============================================================================


def test_e2e_openai_client_reads_environment_variables(mock_env_vars):
    """Test OpenAI client reads TLS config from environment variables."""
    with mock.patch("kagent.adk.models._openai.create_ssl_context") as mock_create_ssl:
        with mock.patch("httpx.AsyncClient") as mock_httpx:
            with mock.patch("kagent.adk.models._openai.AsyncOpenAI"):
                mock_create_ssl.return_value = mock.MagicMock(spec=ssl.SSLContext)
                mock_httpx.return_value = mock.MagicMock()

                # Create OpenAI client without explicit TLS params
                openai_llm = OpenAI(model="gpt-3.5-turbo", type="openai", api_key="test-key")

                # Trigger client creation
                _ = openai_llm._client

                # Verify environment variables were read
                mock_create_ssl.assert_called_once()
                call_kwargs = mock_create_ssl.call_args[1]
                assert call_kwargs["verify_disabled"] is False
                assert call_kwargs["ca_cert_path"] == mock_env_vars["TLS_CA_CERT_PATH"]
                assert call_kwargs["use_system_cas"] is True


# ============================================================================
# Integration Test 8: Certificate Validation with Expiry Warnings
# ============================================================================


def test_e2e_certificate_validation_expiry_warnings(caplog):
    """Test certificate validation logs expiry warnings but doesn't block."""
    # This test requires the cryptography library to be installed
    try:
        from cryptography import x509
        from cryptography.hazmat.backends import default_backend
        from cryptography.hazmat.primitives import hashes, serialization
        from cryptography.hazmat.primitives.asymmetric import rsa
        from cryptography.x509.oid import NameOID
        from datetime import datetime, timedelta, timezone

        # Generate an expired certificate
        key = rsa.generate_private_key(public_exponent=65537, key_size=2048, backend=default_backend())
        subject = issuer = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Test CA")])

        # Create certificate that expired 1 day ago
        cert = (
            x509.CertificateBuilder()
            .subject_name(subject)
            .issuer_name(issuer)
            .public_key(key.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(datetime.now(timezone.utc) - timedelta(days=365))
            .not_valid_after(datetime.now(timezone.utc) - timedelta(days=1))  # Expired
            .sign(key, hashes.SHA256(), default_backend())
        )

        # Write to temporary file
        with tempfile.NamedTemporaryFile(mode="wb", suffix=".crt", delete=False) as f:
            f.write(cert.public_bytes(serialization.Encoding.PEM))
            expired_cert_path = f.name

        try:
            with caplog.at_level(logging.WARNING):
                validate_certificate(expired_cert_path)

                # Verify expiry warning was logged
                assert "EXPIRED" in caplog.text
        finally:
            Path(expired_cert_path).unlink(missing_ok=True)

    except ImportError:
        pytest.skip("cryptography library not installed - skipping certificate validation test")


# ============================================================================
# Integration Test 9: Structured Logging at Startup
# ============================================================================


def test_e2e_structured_logging_at_startup(temp_cert_file, caplog):
    """Test that TLS configuration logs structured information at startup."""
    with caplog.at_level(logging.INFO):
        with mock.patch("ssl.create_default_context") as mock_default_ctx:
            with mock.patch.object(ssl.SSLContext, "load_verify_locations"):
                mock_ctx = mock.MagicMock()
                mock_default_ctx.return_value = mock_ctx

                create_ssl_context(
                    verify_disabled=False,
                    ca_cert_path=temp_cert_file,
                    use_system_cas=True,
                )

                # Verify structured logging messages
                log_text = caplog.text
                assert "TLS Mode:" in log_text
                assert "Custom CA + System CAs" in log_text
                assert "Using system CA certificates" in log_text
                assert "Custom CA certificate loaded from:" in log_text
                assert temp_cert_file in log_text


# ============================================================================
# Integration Test 10: Complete Flow with LiteLLM Base URL
# ============================================================================


def test_e2e_litellm_with_tls(temp_cert_file):
    """Test complete flow: LiteLLM base URL + TLS configuration."""
    with mock.patch("kagent.adk.models._openai.create_ssl_context") as mock_create_ssl:
        with mock.patch("httpx.AsyncClient") as mock_httpx:
            with mock.patch("kagent.adk.models._openai.AsyncOpenAI") as mock_openai:
                mock_ssl_context = mock.MagicMock(spec=ssl.SSLContext)
                mock_create_ssl.return_value = mock_ssl_context
                mock_httpx_instance = mock.MagicMock()
                mock_httpx.return_value = mock_httpx_instance

                # Create OpenAI client pointing to LiteLLM with TLS
                openai_llm = OpenAI(
                    model="gpt-3.5-turbo",
                    type="openai",
                    api_key="test-key",
                    base_url="https://litellm.internal.corp:8080",
                    tls_ca_cert_path=temp_cert_file,
                    tls_use_system_cas=True,
                )

                # Trigger client creation
                _ = openai_llm._client

                # Verify complete integration:
                # 1. SSL context created with custom CA
                mock_create_ssl.assert_called_once_with(
                    verify_disabled=False,
                    ca_cert_path=temp_cert_file,
                    use_system_cas=True,
                )

                # 2. httpx client created with SSL context
                mock_httpx.assert_called_once()
                httpx_kwargs = mock_httpx.call_args[1]
                assert httpx_kwargs["verify"] is mock_ssl_context

                # 3. AsyncOpenAI created with custom http_client and base_url
                mock_openai.assert_called_once()
                openai_kwargs = mock_openai.call_args[1]
                assert openai_kwargs["http_client"] is mock_httpx_instance
                assert openai_kwargs["base_url"] == "https://litellm.internal.corp:8080"
