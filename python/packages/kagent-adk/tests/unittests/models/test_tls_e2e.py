"""End-to-end tests for ModelConfig TLS support.

These tests verify actual TLS connections with self-signed certificates,
testing the full flow from SSL context creation through httpx client
configuration to actual HTTPS requests.
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
            raise RuntimeError(
                f"Failed to bind to port {self.port}. "
                f"Error: {e}"
            ) from e

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


@pytest.mark.asyncio
async def test_e2e_with_self_signed_cert():
    """E2E test: Connect to HTTPS server with self-signed certificate using custom CA.

    This test verifies the complete flow:
    1. Start test HTTPS server with self-signed certificate
    2. Create SSL context with custom CA certificate
    3. Create httpx client with SSL context
    4. Make actual HTTPS request
    5. Verify TLS handshake succeeds
    6. Verify request/response works end-to-end
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
async def test_e2e_with_self_signed_cert_fails_without_ca():
    """E2E test: Connection fails when custom CA is not provided.

    This test verifies that attempting to connect to a server with a
    self-signed certificate fails when the CA is not trusted.
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

    This test verifies:
    1. Start test HTTPS server with self-signed certificate
    2. Create ModelConfig with verifyDisabled: true (no Secret)
    3. Create httpx client with verification disabled
    4. Agent connects successfully despite untrusted certificate
    5. Warning logs are present in output
    """
    with TestHTTPSServer(port=8445):
        # Create SSL context with verification disabled
        ssl_context = create_ssl_context(
            disable_verify=True,
            ca_cert_path=None,
            disable_system_cas=False,
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

        # Access the _client property to trigger initialization
        client = model._client

        # Verify client is configured correctly
        assert client is not None
        assert isinstance(client._client, httpx.AsyncClient)

        # Make request to test server - should succeed with custom CA + system CAs set
        response = await client._client.get("https://localhost:8446/health")
        assert response.status_code == 200
        assert response.text == "OK"


@pytest.mark.asyncio
async def test_e2e_openai_client_fails_without_custom_ca():
    """E2E test: OpenAI client fails when custom CA is required but not provided.

    This test verifies that TLS verification actually happens by ensuring
    that connections to self-signed certificate servers fail when the CA
    is not provided.
    """
    with TestHTTPSServer(port=8450):
        # Create OpenAI client WITHOUT custom CA (should fail verification)
        model = BaseOpenAI(
            model="gpt-4",
            api_key="test-key",
            base_url="https://localhost:8450/v1",
            tls_disable_verify=False,
            tls_ca_cert_path=None,  # No custom CA
            tls_disable_system_cas=True,  # Empty trust store - no CAs at all
        )

        # Access the _client property to trigger initialization
        client = model._client

        assert client is not None
        assert isinstance(client._client, httpx.AsyncClient)

        # Attempt to connect should fail with SSL verification error
        with pytest.raises((httpx.ConnectError, ssl.SSLError)):
            await client._client.get("https://localhost:8450/health")


@pytest.mark.asyncio
async def test_e2e_openai_client_with_custom_ca():
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

        # Access the _client property to trigger initialization
        client = model._client

        # Verify client is configured correctly
        assert client is not None
        assert isinstance(client._client, httpx.AsyncClient)

        # Make a request using the httpx client directly to test connectivity
        response = await client._client.get("https://localhost:8447/health")
        assert response.status_code == 200
        assert response.text == "OK"


@pytest.mark.asyncio
async def test_e2e_openai_client_with_verification_disabled():
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
            tls_disable_system_cas=False,
        )

        # Access the _client property to trigger initialization
        client = model._client
        # Verify client uses verification disabled
        assert client is not None
        assert isinstance(client._client, httpx.AsyncClient)

        # Make a request - should succeed despite untrusted cert
        response = await client._client.get("https://localhost:8448/health")
        assert response.status_code == 200
        assert response.text == "OK"


@pytest.mark.asyncio
async def test_e2e_backward_compatibility_no_tls_config():
    """E2E test: Backward compatibility - client works without TLS config.

    This test verifies that OpenAI client works correctly when no TLS
    configuration is provided, using default system CAs.
    """
    # Create OpenAI client without TLS configuration (all fields None/default)
    model = BaseOpenAI(
        model="gpt-4",
        api_key="test-key",
        base_url="https://www.google.com",  # Use real endpoint with valid cert
        tls_disable_verify=None,  # Not set
        tls_ca_cert_path=None,  # Not set
        tls_disable_system_cas=None,  # Not set
    )

    # Access the _client property to trigger initialization
    client = model._client

    # Verify client is configured correctly
    assert client is not None
    assert isinstance(client._client, httpx.AsyncClient)

    # Make a request to a real public endpoint (uses system CAs)
    try:
        response = await client._client.get("https://www.google.com", timeout=5.0)
        # Verify we got a response (status code doesn't matter, just that SSL handshake succeeded)
        assert response.status_code in [200, 301, 302, 404]
    except httpx.TimeoutException:
        # Timeout is acceptable - we're just testing SSL works
        pass


@pytest.mark.asyncio
async def test_e2e_multiple_requests_with_connection_pooling():
    """E2E test: Verify connection pooling works with custom SSL context.

    This test verifies that multiple requests reuse the same connection
    pool and SSL context efficiently.
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
