"""Gemini SDK requests through a custom HTTPS gateway, without cloud credentials."""

import json
import select
import socket
import ssl
import threading
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock
from urllib.parse import urlparse

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID
from google.adk.models.llm_request import LlmRequest
from google.genai import types

from kagent.adk.types import Gemini, ModelConfig, _create_llm_from_model_config


@pytest.fixture
def native_gateway(tmp_path):
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "localhost")])
    now = datetime.now(timezone.utc)
    cert = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(subject)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - timedelta(minutes=1))
        .not_valid_after(now + timedelta(days=1))
        .sign(key, hashes.SHA256())
    )
    cert_file, key_file = tmp_path / "cert.pem", tmp_path / "key.pem"
    cert_file.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    key_file.write_bytes(
        key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption())
    )
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def do_POST(self):
            requests.append(
                (self.path, dict(self.headers), json.loads(self.rfile.read(int(self.headers["Content-Length"]))))
            )
            content_type = "application/json"
            body = json.dumps(
                {"candidates": [{"content": {"role": "model", "parts": [{"text": "ok"}]}, "finishReason": "STOP"}]}
            ).encode()
            if ":streamGenerateContent" in self.path:
                content_type, body = "text/event-stream", b"data: " + body + b"\n\n"
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert_file, key_file)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"https://127.0.0.1:{server.server_port}", requests
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


@pytest.mark.parametrize(
    "model_id,encoded",
    [
        ("catalog-alias", "catalog-alias"),
        ("team/gemini-listed", "team%2Fgemini-listed"),
        ("models/gemini-listed", "models%2Fgemini-listed"),
        ("tunedModels/custom", "tunedModels%2Fcustom"),
        ("team/model:variant", "team%2Fmodel%3Avariant"),
        ("team%2Fmodel", "team%252Fmodel"),
    ],
)
@pytest.mark.parametrize("stream", [False, True])
@pytest.mark.parametrize("trailing_slash", ["", "/"])
async def test_gemini_custom_endpoint(native_gateway, monkeypatch, stream, trailing_slash, model_id, encoded):
    monkeypatch.delenv("GOOGLE_API_KEY", raising=False)
    monkeypatch.delenv("GEMINI_API_KEY", raising=False)
    monkeypatch.setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
    base, requests = native_gateway
    prefix = "/team/v1"
    expected_headers = {"x-route": "team"}
    if model_id == "team/gemini-listed":
        expected_headers["authorization"] = "Bearer kagent-credential-injected"
    elif trailing_slash:
        expected_headers["x-goog-api-key"] = "kagent-credential-injected"
    config = ModelConfig(
        model=model_id,
        headers=expected_headers,
        tls_insecure_skip_verify=True,
        gemini=Gemini(base_url=base + prefix + trailing_slash),
    )
    llm = _create_llm_from_model_config(config)
    request = LlmRequest(model=model_id, contents=[types.Content(role="user", parts=[types.Part(text="hello")])])
    try:
        responses = [r async for r in llm.generate_content_async(request, stream=stream)]
        assert all(not response.error_message for response in responses)
        assert any(part.text == "ok" for r in responses if r.content for part in r.content.parts or [])
        assert len(requests) == 1
        path, headers, body = requests[0]
        headers = {k.lower(): v for k, v in headers.items()}
        assert headers["x-route"] == "team"
        assert headers.get("x-goog-api-key") == expected_headers.get("x-goog-api-key")
        assert headers.get("authorization") == expected_headers.get("authorization")
        operation = "streamGenerateContent?alt=sse" if stream else "generateContent"
        assert path == f"{prefix}/models/{encoded}:{operation}"
        assert "contents" in body
        assert request.model == model_id
        assert llm.model == model_id
    finally:
        await llm.api_client.aio.aclose()
        llm.api_client.close()


def test_gemini_direct_keeps_provider_credentials(monkeypatch):
    monkeypatch.setenv("GOOGLE_API_KEY", "direct-provider-key")
    llm = _create_llm_from_model_config(ModelConfig(model="gemini-test", gemini=Gemini()))
    with mock.patch("kagent.adk.models._gemini.Client") as client:
        _ = llm.api_client
    assert client.call_args.kwargs["api_key"] == "direct-provider-key"
    assert client.call_args.kwargs["http_options"].api_version is None
    assert client.call_args.kwargs["http_options"].base_url is None


@pytest.mark.parametrize("stream", [False, True])
@pytest.mark.parametrize("scheme", ["http", "https", "default"])
async def test_gemini_custom_endpoint_uses_environment_proxy(native_gateway, monkeypatch, stream, scheme):
    default_endpoint = scheme == "default"
    if default_endpoint:
        scheme = "https"
    monkeypatch.delenv("GOOGLE_API_KEY", raising=False)
    monkeypatch.delenv("GEMINI_API_KEY", raising=False)
    requests = []
    backend, backend_requests = native_gateway
    backend_url = urlparse(backend)

    class Proxy(BaseHTTPRequestHandler):
        def do_CONNECT(self):
            requests.append((self.path, dict(self.headers)))
            with socket.create_connection((backend_url.hostname, backend_url.port), timeout=5) as upstream:
                self.send_response(200)
                self.end_headers()
                while True:
                    ready, _, _ = select.select([self.connection, upstream], [], [], 5)
                    if not ready:
                        return
                    for source in ready:
                        data = source.recv(65536)
                        if not data:
                            return
                        destination = upstream if source is self.connection else self.connection
                        destination.sendall(data)

        def log_message(self, *_args):
            pass

        def do_POST(self):
            requests.append((self.path, dict(self.headers)))
            self.rfile.read(int(self.headers["Content-Length"]))
            body = json.dumps(
                {"candidates": [{"content": {"role": "model", "parts": [{"text": "ok"}]}, "finishReason": "STOP"}]}
            ).encode()
            content_type = "application/json"
            if stream:
                content_type, body = "text/event-stream", b"data: " + body + b"\n\n"
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    proxy = ThreadingHTTPServer(("127.0.0.1", 0), Proxy)
    thread = threading.Thread(target=proxy.serve_forever, daemon=True)
    thread.start()
    monkeypatch.setenv(f"{scheme.upper()}_PROXY", f"http://127.0.0.1:{proxy.server_port}")
    monkeypatch.setenv("NO_PROXY", "")
    monkeypatch.delenv(f"{scheme}_proxy", raising=False)
    monkeypatch.delenv("no_proxy", raising=False)
    llm = _create_llm_from_model_config(
        ModelConfig(
            model="gemini-test" if default_endpoint else "team/model",
            headers={"x-route": "team", "Authorization": "Bearer actor-placeholder"},
            gemini=Gemini() if default_endpoint else Gemini(base_url=f"{scheme}://unresolvable.invalid/team/v1"),
            tls_insecure_skip_verify=True,
        )
    )
    try:
        request = LlmRequest(model=llm.model, contents=[types.Content(role="user", parts=[types.Part(text="hello")])])
        responses = [r async for r in llm.generate_content_async(request, stream=stream)]
        assert any(part.text == "ok" for r in responses if r.content for part in r.content.parts or [])
        assert len(requests) == 1
        path, headers = requests[0]
        operation = "streamGenerateContent?alt=sse" if stream else "generateContent"
        if scheme == "https":
            assert path == ("generativelanguage.googleapis.com:443" if default_endpoint else "unresolvable.invalid:443")
            assert len(backend_requests) == 1
            path, headers, _ = backend_requests[0]
            assert path == (
                f"/v1beta/models/gemini-test:{operation}"
                if default_endpoint
                else f"/team/v1/models/team%2Fmodel:{operation}"
            )
        else:
            assert path == f"http://unresolvable.invalid/team/v1/models/team%2Fmodel:{operation}"
        headers = {key.lower(): value for key, value in headers.items()}
        assert headers["x-route"] == "team"
        assert headers["authorization"] == "Bearer actor-placeholder"
        assert "x-goog-api-key" not in headers
    finally:
        await llm.api_client.aio.aclose()
        llm.api_client.close()
        proxy.shutdown()
        proxy.server_close()
        thread.join()


@pytest.mark.parametrize("base_url", [None, "https://models.example.com/v1"])
def test_gemini_endpoint_preserves_api_key(monkeypatch, base_url):
    monkeypatch.setenv("GOOGLE_API_KEY", "configured-key")
    llm = _create_llm_from_model_config(ModelConfig(model="gemini-test", gemini=Gemini(base_url=base_url)))
    with mock.patch("kagent.adk.models._gemini.Client") as client:
        _ = llm.api_client
    assert client.call_args.kwargs["api_key"] == "configured-key"
