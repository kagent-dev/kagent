"""Unit tests for the kagent TLS wiring on MCP server wire configs.

The kagent controller emits TLS keys nested under ``params`` on the
wire (matching the Go runtime's shape). kagent-adk lifts those keys up
to the wire-config level via a pydantic ``model_validator(mode="before")``
and installs a TLS-aware ``httpx_client_factory`` on the underlying
google-adk params instance at toolset construction time, so every MCP
session the manager opens applies the operator's TLS config.

These tests exercise the lift and the factory installation without
standing up a real MCP server.
"""

from unittest import mock

import httpx
import pytest
from google.adk.tools.mcp_tool import SseConnectionParams, StreamableHTTPConnectionParams

from kagent.adk.types import HttpMcpServerConfig, SseMcpServerConfig


def test_lifts_tls_fields_from_nested_params_on_streamable():
    """The controller emits ``{"params": {"url": ..., "tls_ca_cert_path": ...}}``;
    the pre-validator lifts the TLS keys onto the wire-config object so
    the runtime can read them without digging into google-adk's params
    type (whose ``extra="ignore"`` would silently drop them)."""
    cfg = HttpMcpServerConfig.model_validate(
        {
            "params": {
                "url": "https://upstream.example.com/mcp",
                "tls_insecure_skip_verify": True,
                "tls_ca_cert_path": "/etc/ssl/certs/custom/corp-ca/ca.crt",
                "tls_disable_system_cas": False,
            },
            "tools": ["t1"],
        }
    )
    assert cfg.tls_insecure_skip_verify is True
    assert cfg.tls_ca_cert_path == "/etc/ssl/certs/custom/corp-ca/ca.crt"
    assert cfg.tls_disable_system_cas is False


def test_lifts_tls_fields_from_nested_params_on_sse():
    """SSE wire-config gets the same lift behavior as Streamable HTTP so
    operators don't need to know which transport is in use."""
    cfg = SseMcpServerConfig.model_validate(
        {
            "params": {
                "url": "https://upstream.example.com/sse",
                "tls_insecure_skip_verify": False,
                "tls_ca_cert_path": "/etc/ssl/certs/custom/corp-ca/ca.crt",
            },
        }
    )
    assert cfg.tls_insecure_skip_verify is False
    assert cfg.tls_ca_cert_path == "/etc/ssl/certs/custom/corp-ca/ca.crt"


def test_lift_respects_explicit_top_level_value():
    """When the wire config already carries an explicit top-level TLS
    value, the nested-params lift must not override it. Production never
    emits both, but a Python caller building configs directly might."""
    cfg = HttpMcpServerConfig.model_validate(
        {
            "params": {
                "url": "https://upstream.example.com/mcp",
                "tls_ca_cert_path": "/from/params",
            },
            "tools": [],
            "tls_ca_cert_path": "/from/top-level",
        }
    )
    assert cfg.tls_ca_cert_path == "/from/top-level"


def test_no_tls_config_leaves_factory_default():
    """When no TLS fields are set, ``_apply_tls_to_params`` is a no-op
    so google-adk's upstream default factory remains in place. This is
    the common case for any RemoteMCPServer without ``spec.tls`` set."""
    params = StreamableHTTPConnectionParams(url="https://upstream.example.com/mcp")
    original_factory = params.httpx_client_factory

    cfg = HttpMcpServerConfig(params=params, tools=[])
    cfg._apply_tls_to_params(cfg.params)

    assert cfg.params.httpx_client_factory is original_factory


def test_disable_verify_installs_factory_with_verify_false():
    """``tls_insecure_skip_verify=True`` is the test-fixture escape hatch.
    The factory it installs must hand httpx ``verify=False`` so self-signed
    upstreams accept the connection."""
    params = StreamableHTTPConnectionParams(url="https://upstream.example.com/mcp")
    cfg = HttpMcpServerConfig(
        params=params,
        tools=[],
        tls_insecure_skip_verify=True,
    )

    with mock.patch("kagent.adk.types.create_ssl_context") as mock_create:
        mock_create.return_value = False  # httpx accepts False to disable
        cfg._apply_tls_to_params(cfg.params)

    mock_create.assert_called_once_with(
        disable_verify=True,
        ca_cert_path=None,
        disable_system_cas=False,
    )

    # Calling the installed factory should produce an httpx.AsyncClient
    # configured with the SSL context returned by create_ssl_context.
    with mock.patch("kagent.adk.types.httpx.AsyncClient") as mock_client:
        cfg.params.httpx_client_factory()
        kwargs = mock_client.call_args[1]
        assert kwargs["verify"] is False


def test_custom_ca_path_installs_factory_with_ssl_context():
    """The production case: ``tls_ca_cert_path`` pins a corporate CA. The
    factory must call ``create_ssl_context`` with the same path so the
    resulting SSL context trusts that CA."""
    params = StreamableHTTPConnectionParams(url="https://mcp.corp.internal/mcp")
    cfg = HttpMcpServerConfig(
        params=params,
        tools=[],
        tls_ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
    )

    fake_ctx = object()  # stand-in for an ssl.SSLContext
    with mock.patch("kagent.adk.types.create_ssl_context") as mock_create:
        mock_create.return_value = fake_ctx
        cfg._apply_tls_to_params(cfg.params)

    mock_create.assert_called_once_with(
        disable_verify=False,
        ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
        disable_system_cas=False,
    )

    with mock.patch("kagent.adk.types.httpx.AsyncClient") as mock_client:
        cfg.params.httpx_client_factory()
        kwargs = mock_client.call_args[1]
        assert kwargs["verify"] is fake_ctx
        # Caller defaults preserved: follow_redirects + timeout matching
        # google-adk's create_mcp_http_client defaults.
        assert kwargs["follow_redirects"] is True
        assert isinstance(kwargs["timeout"], httpx.Timeout)


def test_disable_system_cas_propagates_to_create_ssl_context():
    """``tls_disable_system_cas=True`` is the strict-trust mode: the SSL
    context should trust ONLY the supplied CA. The wire-config must pass
    the flag through to ``create_ssl_context`` without rewriting."""
    params = StreamableHTTPConnectionParams(url="https://mcp.corp.internal/mcp")
    cfg = HttpMcpServerConfig(
        params=params,
        tools=[],
        tls_ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
        tls_disable_system_cas=True,
    )

    with mock.patch("kagent.adk.types.create_ssl_context") as mock_create:
        cfg._apply_tls_to_params(cfg.params)

    mock_create.assert_called_once_with(
        disable_verify=False,
        ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
        disable_system_cas=True,
    )


def test_factory_forwards_caller_kwargs():
    """google-adk's MCP session manager invokes the factory with optional
    headers/timeout/auth on a per-session basis. The TLS-wrapping factory
    must forward those kwargs so per-session overrides still work."""
    params = StreamableHTTPConnectionParams(url="https://mcp.corp.internal/mcp")
    cfg = HttpMcpServerConfig(
        params=params,
        tools=[],
        tls_ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
    )
    with mock.patch("kagent.adk.types.create_ssl_context", return_value=object()):
        cfg._apply_tls_to_params(cfg.params)

    auth = httpx.BasicAuth(username="u", password="p")
    timeout = httpx.Timeout(60)
    with mock.patch("kagent.adk.types.httpx.AsyncClient") as mock_client:
        cfg.params.httpx_client_factory(
            headers={"X-Token": "abc"},
            timeout=timeout,
            auth=auth,
        )

    kwargs = mock_client.call_args[1]
    assert kwargs["headers"] == {"X-Token": "abc"}
    assert kwargs["timeout"] is timeout
    assert kwargs["auth"] is auth


def test_sse_params_get_factory_when_supported():
    """SSE transport should receive the same factory treatment when the
    installed google-adk version is recent enough to expose
    ``httpx_client_factory`` on ``SseConnectionParams`` (≥ 1.28.1, the
    kagent-adk pyproject floor)."""
    params = SseConnectionParams(url="https://upstream.example.com/sse")
    cfg = SseMcpServerConfig(
        params=params,
        tools=[],
        tls_ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
    )

    if (
        not hasattr(SseConnectionParams, "model_fields")
        or "httpx_client_factory" not in SseConnectionParams.model_fields
    ):
        pytest.skip("installed google-adk lacks httpx_client_factory on SSE — upgrade to ≥ 1.28.1")

    with mock.patch("kagent.adk.types.create_ssl_context", return_value=object()):
        cfg._apply_tls_to_params(cfg.params)

    assert callable(cfg.params.httpx_client_factory)
