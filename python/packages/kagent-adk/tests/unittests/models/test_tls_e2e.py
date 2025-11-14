"""End-to-end tests for ModelConfig TLS support.

This file contains E2E tests that verify TLS/SSL functionality with real HTTPS
servers and actual network connections. Tests are organized into two categories:

1. SSL Infrastructure Layer Tests (using create_ssl_context() directly):
   These tests verify SSL context creation and httpx client integration with
   real HTTPS servers. They test the SSL infrastructure layer, not BaseOpenAI
   integration. For unit tests of create_ssl_context() function logic, see
   test_ssl.py.

2. BaseOpenAI Integration Tests (using generate_content_async()):
   These tests verify the complete end-to-end flow through BaseOpenAI,
   including TLS configuration, httpx client setup, OpenAI SDK integration,
   and actual API calls via generate_content_async().
"""

import asyncio
import json
import logging
import ssl
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Any

import httpx
import pytest
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai.types import Content, Part

from kagent.adk.models._openai import BaseOpenAI
from kagent.adk.models._ssl import create_ssl_context

# Path to test certificates
CERT_DIR = Path(__file__).parent.parent.parent / "fixtures" / "certs"
CA_CERT = CERT_DIR / "ca-cert.pem"
SERVER_CERT = CERT_DIR / "server-cert.pem"
SERVER_KEY = CERT_DIR / "server-key.pem"


class MockLLMHandler(BaseHTTPRequestHandler):
    """Mock LLM server handler that returns OpenAI-compatible responses."""

    def log_message(self, format: str, *args: Any) -> None:
        """Suppress server logs during testing."""
        pass

    def do_POST(self) -> None:
        """Handle POST requests to /v1/chat/completions."""
        if self.path == "/v1/chat/completions":
            content_length = int(self.headers["Content-Length"])
            body = self.rfile.read(content_length)
            request_data = json.loads(body)

            # Return a mock OpenAI-compatible response
            response = {
                "id": "chatcmpl-test",
                "object": "chat.completion",
                "created": 1234567890,
                "model": request_data.get("model", "gpt-4"),
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": "Hello from test server!"},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
            }

            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(response).encode())
        else:
            self.send_response(404)
            self.end_headers()

    def do_GET(self) -> None:
        """Handle GET requests for health checks."""
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"OK")
        else:
            self.send_response(404)
            self.end_headers()


class TestHTTPSServer:
    """Context manager for running a test HTTPS server in a background thread."""

    def __init__(self, port: int = 8443, use_ssl: bool = True):
        self.port = port
        self.use_ssl = use_ssl
        self.server: HTTPServer | None = None
        self.thread: threading.Thread | None = None

    def __enter__(self) -> "TestHTTPSServer":
        """Start the HTTPS server in a background thread."""
        try:
            self.server = HTTPServer(("localhost", self.port), MockLLMHandler)
        except OSError as e:
            raise RuntimeError(f"Failed to bind to port {self.port}. Error: {e}") from e

        if self.use_ssl:
            # Configure SSL context for server
            ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            ssl_context.load_cert_chain(str(SERVER_CERT), str(SERVER_KEY))
            self.server.socket = ssl_context.wrap_socket(
                self.server.socket,
                server_side=True,
            )

        # Start server in background thread
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

        # Wait for server to be ready
        import time

        time.sleep(0.1)

        return self

    def __exit__(self, *args: Any) -> None:
        """Shutdown the HTTPS server."""
        if self.server:
            self.server.shutdown()
            self.server.server_close()
        if self.thread:
            self.thread.join(timeout=1.0)

    @property
    def url(self) -> str:
        """Get the base URL of the test server."""
        protocol = "https" if self.use_ssl else "http"
        return f"{protocol}://localhost:{self.port}"


# ssl context tests


@pytest.mark.asyncio
async def test_e2e_with_self_signed_cert_with_custom_ca():
    """E2E test: Connect to HTTPS server with self-signed certificate using custom CA.

    This test verifies the SSL infrastructure layer (not BaseOpenAI integration):
    1. Start test HTTPS server with self-signed certificate
    2. Create SSL context with custom CA certificate using create_ssl_context()
    3. Create httpx client with SSL context
    4. Make actual HTTPS request
    5. Verify TLS handshake succeeds
    6. Verify request/response works end-to-end

    Note: This tests SSL context creation and httpx client integration directly.
    For BaseOpenAI integration tests, see tests that use generate_content_async().
    """
    with TestHTTPSServer(port=8443):
        # Create SSL context with custom CA (no system CAs to isolate test)
        ssl_context = create_ssl_context(
            disable_verify=False,
            ca_cert_path=str(CA_CERT),
            disable_system_cas=True,
        )

        # Create httpx client with custom SSL context
        async with httpx.AsyncClient(verify=ssl_context) as client:
            # Make request to test server
            response = await client.get("https://localhost:8443/health")

            # Verify response
            assert response.status_code == 200
            assert response.text == "OK"


@pytest.mark.asyncio
async def test_e2e_with_self_signed_cert_fails_without_custom_ca():
    """E2E test: Connection fails when custom CA is not provided.

    This test verifies the SSL infrastructure layer (not BaseOpenAI integration):
    - Attempting to connect to a server with a self-signed certificate fails
      when the CA is not trusted
    - Tests create_ssl_context() with empty trust store and real HTTPS server

    Note: This tests SSL context creation and httpx client integration directly.
    For BaseOpenAI integration tests, see test_e2e_openai_generate_content_fails_without_custom_ca.
    """
    with TestHTTPSServer(port=8444):
        # Create SSL context WITHOUT custom CA (should fail verification)
        ssl_context = create_ssl_context(
            disable_verify=False,
            ca_cert_path=None,
            disable_system_cas=False,
        )

        # Attempt to connect should fail with SSL verification error
        async with httpx.AsyncClient(verify=ssl_context) as client:
            with pytest.raises(httpx.ConnectError):
                await client.get("https://localhost:8444/health")


@pytest.mark.asyncio
async def test_e2e_with_self_signed_cert_fails_without_custom_ca_or_system_cas():
    """E2E test: Connection fails when custom CA is not provided.

    This test verifies the SSL infrastructure layer (not BaseOpenAI integration):
    - Attempting to connect to a server with a self-signed certificate fails
      when the CA is not trusted
    - Tests create_ssl_context() with empty trust store and real HTTPS server

    Note: This tests SSL context creation and httpx client integration directly.
    For BaseOpenAI integration tests, see test_e2e_openai_generate_content_fails_without_custom_ca.
    """
    with TestHTTPSServer(port=8444):
        # Create SSL context WITHOUT custom CA (should fail verification)
        ssl_context = create_ssl_context(
            disable_verify=False,
            ca_cert_path=None,
            disable_system_cas=True,  # Empty trust store - no CAs at all
        )

        # Attempt to connect should fail with SSL verification error
        async with httpx.AsyncClient(verify=ssl_context) as client:
            with pytest.raises(httpx.ConnectError):
                await client.get("https://localhost:8444/health")


@pytest.mark.asyncio
async def test_e2e_with_verification_disabled():
    """E2E test: Connect successfully with verification disabled.

    This test verifies the SSL infrastructure layer (not BaseOpenAI integration):
    1. Start test HTTPS server with self-signed certificate
    2. Create SSL context with verification disabled using create_ssl_context()
    3. Create httpx client with verification disabled
    4. Client connects successfully despite untrusted certificate
    5. Verify that False is returned (httpx special value for disabled verification)

    Note: This tests SSL context creation and httpx client integration directly.
    For BaseOpenAI integration tests, see test_e2e_openai_client_with_verification_disabled.
    """
    with TestHTTPSServer(port=8445):
        # Create SSL context with verification disabled
        ssl_context = create_ssl_context(
            disable_verify=True,
            ca_cert_path=None,
            disable_system_cas=True,
        )

        # Verify that False is returned (httpx special value)
        assert ssl_context is False

        # Create httpx client with verification disabled
        async with httpx.AsyncClient(verify=False) as client:
            # Make request to test server - should succeed despite untrusted cert
            response = await client.get("https://localhost:8445/health")

            # Verify response
            assert response.status_code == 200
            assert response.text == "OK"


@pytest.mark.asyncio
async def test_e2e_with_verification_disabled_logs_warning(caplog):
    """E2E test: Verify warning logs when verification is disabled."""
    with caplog.at_level(logging.WARNING):
        _ = create_ssl_context(
            disable_verify=True,
            ca_cert_path=None,
            disable_system_cas=False,
        )

        # Verify warning was logged
        assert "SSL VERIFICATION DISABLED" in caplog.text
        assert "development/testing" in caplog.text.lower()


# baseopenai tests


@pytest.mark.asyncio
async def test_e2e_with_system_and_custom_ca():
    """E2E test: Connect with both system CAs and custom CA.

    This test verifies the additive behavior where both system CAs
    and custom CA are trusted.
    """
    with TestHTTPSServer(port=8446):
        # Create OpenAI client with both system and custom CAs
        model = BaseOpenAI(
            model="gpt-4",
            api_key="test-key",
            base_url="https://localhost:8446/v1",
            tls_disable_verify=False,
            tls_ca_cert_path=str(CA_CERT),
            tls_disable_system_cas=False,  # Use both system and custom CAs
        )

        # Create a proper LlmRequest
        llm_request = LlmRequest(
            model="gpt-4",
            contents=[Content(role="user", parts=[Part.from_text(text="Hello, test!")])],
        )

        # Call generate_content_async() - should succeed with custom CA + system CAs
        responses = [resp async for resp in model.generate_content_async(llm_request, stream=False)]

        # Verify we got a successful response
        assert len(responses) == 1
        assert isinstance(responses[0], LlmResponse)
        assert responses[0].content is not None
        assert len(responses[0].content.parts) > 0
        assert responses[0].content.parts[0].text == "Hello from test server!"


@pytest.mark.asyncio
async def test_e2e_fails_without_custom_ca():
    """E2E test: generate_content_async() fails when custom CA is required but not provided.

    This test verifies the complete end-to-end flow including TLS verification:
    1. Start test HTTPS server with self-signed certificate
    2. Create BaseOpenAI model WITHOUT custom CA (empty trust store)
    3. Call generate_content_async() which should fail due to SSL verification
    4. Verify the error is properly handled
    """
    with TestHTTPSServer(port=8452):
        # Create OpenAI client WITHOUT custom CA (should fail verification)
        model = BaseOpenAI(
            model="gpt-4",
            api_key="test-key",
            base_url="https://localhost:8452/v1",
            tls_disable_verify=False,
            tls_ca_cert_path=None,  # No custom CA
            tls_disable_system_cas=True,  # Empty trust store - no CAs at all
        )

        # Create a proper LlmRequest
        llm_request = LlmRequest(
            model="gpt-4",
            contents=[Content(role="user", parts=[Part.from_text(text="Hello, test!")])],
        )

        # Call generate_content_async() - should fail with SSL error
        responses = [resp async for resp in model.generate_content_async(llm_request, stream=False)]

        # Verify we got an error response
        assert len(responses) == 1
        assert isinstance(responses[0], LlmResponse)
        assert responses[0].error_code == "API_ERROR"
        assert responses[0].error_message is not None
        # The error message should indicate a connection/SSL failure
        # (exact format may vary, but should indicate the connection failed)
        error_msg_lower = responses[0].error_message.lower()
        assert any(
            keyword in error_msg_lower
            for keyword in [
                "ssl",
                "tls",
                "certificate",
                "verify",
                "handshake",
                "connect",
                "connection",
                "failed",
            ]
        )


@pytest.mark.asyncio
async def test_e2e_with_custom_ca_no_system_cas():
    """E2E test: OpenAI client with custom CA certificate.

    This test verifies the complete integration with OpenAI client:
    1. Start test HTTPS server that mimics LiteLLM/OpenAI API
    2. Create BaseOpenAI model with TLS configuration
    3. Make actual API call through OpenAI SDK
    4. Verify response works end-to-end
    """
    with TestHTTPSServer(port=8447):
        # Create OpenAI client with custom TLS configuration
        model = BaseOpenAI(
            model="gpt-4",
            api_key="test-key",
            base_url="https://localhost:8447/v1",
            tls_disable_verify=False,
            tls_ca_cert_path=str(CA_CERT),
            tls_disable_system_cas=True,
        )

        # Create a proper LlmRequest
        llm_request = LlmRequest(
            model="gpt-4",
            contents=[Content(role="user", parts=[Part.from_text(text="Hello, test!")])],
        )

        # Call generate_content_async() - should succeed with custom CA
        responses = [resp async for resp in model.generate_content_async(llm_request, stream=False)]

        # Verify we got a successful response
        assert len(responses) == 1
        assert isinstance(responses[0], LlmResponse)
        assert responses[0].content is not None
        assert len(responses[0].content.parts) > 0
        assert responses[0].content.parts[0].text == "Hello from test server!"


@pytest.mark.skip(
    reason="We'll need to figure out how to properly verify the backward compatibility (all None) without being able to verify against api.openai.com (which the default client properly configures)"
)
@pytest.mark.asyncio
async def test_e2e_backward_compatibility_default_behavior():
    """E2E test: Backward compatibility - default behavior when minimal TLS config is provided.

    This test verifies backward compatibility: when only a custom CA is provided
    (required for our test server) and other TLS fields are not set, the default
    behavior is used (system CAs enabled, verification enabled).

    This simulates the backward compatibility scenario:
    - Before TLS support: clients used system CAs (default Python/httpx behavior)
    - After TLS support: when only custom CA is provided, system CAs are still
      enabled by default (tls_disable_system_cas defaults to False)
    - This ensures existing behavior is preserved

    Note: We can't test with zero TLS config because our test server requires
    a custom CA. But we test that defaults work correctly (system CAs enabled).
    """
    with TestHTTPSServer(port=8453):
        # Create OpenAI client with minimal TLS config (only custom CA for test server)
        # tls_disable_verify and tls_disable_system_cas are NOT set (use defaults)
        model = BaseOpenAI(
            model="gpt-4",
            api_key="test-key",
            base_url="https://localhost:8453/v1",
            tls_disable_verify=None,  # Not set - defaults to False (verification enabled)
            tls_ca_cert_path=None,
            tls_disable_system_cas=None,  # Not set - defaults to False (system CAs enabled)
        )

        # Create a proper LlmRequest
        llm_request = LlmRequest(
            model="gpt-4",
            contents=[Content(role="user", parts=[Part.from_text(text="Hello, test!")])],
        )

        # Call generate_content_async() - should succeed with default behavior
        # (system CAs + custom CA, verification enabled)
        responses = [resp async for resp in model.generate_content_async(llm_request, stream=False)]

        # Verify we got a successful response
        assert len(responses) == 1
        assert isinstance(responses[0], LlmResponse)
        assert responses[0].content is not None
        assert len(responses[0].content.parts) > 0
        assert responses[0].content.parts[0].text == "Hello from test server!"


@pytest.mark.asyncio
async def test_e2e_with_verification_disabled_no_system_cas():
    """E2E test: OpenAI client with verification disabled.

    This test verifies that OpenAI client can connect to servers with
    untrusted certificates when verification is disabled.
    """
    with TestHTTPSServer(port=8448):
        # Create OpenAI client with verification disabled
        model = BaseOpenAI(
            model="gpt-4",
            api_key="test-key",
            base_url="https://localhost:8448/v1",
            tls_disable_verify=True,
            tls_ca_cert_path=None,
            tls_disable_system_cas=True,
        )

        # Create a proper LlmRequest
        llm_request = LlmRequest(
            model="gpt-4",
            contents=[Content(role="user", parts=[Part.from_text(text="Hello, test!")])],
        )

        # Call generate_content_async() - should succeed despite untrusted cert
        responses = [resp async for resp in model.generate_content_async(llm_request, stream=False)]

        # Verify we got a successful response
        assert len(responses) == 1
        assert isinstance(responses[0], LlmResponse)
        assert responses[0].content is not None
        assert len(responses[0].content.parts) > 0
        assert responses[0].content.parts[0].text == "Hello from test server!"


@pytest.mark.asyncio
async def test_e2e_multiple_requests_with_connection_pooling():
    """E2E test: Verify connection pooling works with custom SSL context.

    This test verifies the SSL infrastructure layer (not BaseOpenAI integration):
    - Multiple requests reuse the same connection pool and SSL context efficiently
    - Tests create_ssl_context() with real HTTPS server and connection reuse

    Note: This tests SSL context creation and httpx client integration directly.
    For BaseOpenAI integration tests, see tests that use generate_content_async().
    """
    with TestHTTPSServer(port=8449):
        # Create SSL context with custom CA
        ssl_context = create_ssl_context(
            disable_verify=False,
            ca_cert_path=str(CA_CERT),
            disable_system_cas=True,
        )

        # Create httpx client with connection pooling
        async with httpx.AsyncClient(verify=ssl_context) as client:
            # Make multiple requests
            responses = await asyncio.gather(
                client.get("https://localhost:8449/health"),
                client.get("https://localhost:8449/health"),
                client.get("https://localhost:8449/health"),
            )

            # Verify all requests succeeded
            for response in responses:
                assert response.status_code == 200
                assert response.text == "OK"


@pytest.mark.asyncio
async def test_e2e_ssl_error_contains_troubleshooting_info():
    """E2E test: Verify SSL errors include troubleshooting information.

    This test verifies that when an SSL error occurs, the error message
    includes helpful troubleshooting steps.
    """
    from kagent.adk.models._ssl import get_ssl_troubleshooting_message

    # Create a mock SSL error
    error = ssl.SSLError("certificate verify failed: self signed certificate")

    # Generate troubleshooting message
    message = get_ssl_troubleshooting_message(
        error=error,
        ca_cert_path="/etc/ssl/certs/custom/ca.crt",
        server_url="localhost:8443",
    )

    # Verify message contains helpful information
    assert "SSL/TLS Connection Error" in message
    assert "certificate verify failed" in message
    assert "kubectl exec" in message
    assert "openssl x509" in message
    assert "openssl s_client" in message
    assert "/etc/ssl/certs/custom/ca.crt" in message
    assert "localhost:8443" in message
    assert "kagent.dev/docs" in message


if __name__ == "__main__":
    # Run tests with pytest
    pytest.main([__file__, "-v", "-s"])
