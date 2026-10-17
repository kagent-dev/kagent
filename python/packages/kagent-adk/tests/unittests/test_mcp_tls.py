"""Generated MCP settings preserve SDK defaults and TLS transport behavior."""

from unittest import mock

import httpx2
import pytest
from google.adk.tools.mcp_tool import SseConnectionParams, StreamableHTTPConnectionParams
from kagent.config.v1.adk_pb2 import ADKHTTPConnectionParams, ADKSSEConnectionParams

from kagent.adk.types import _mcp_params


@pytest.mark.parametrize(
    "config_cls,params_cls",
    [(ADKHTTPConnectionParams, StreamableHTTPConnectionParams), (ADKSSEConnectionParams, SseConnectionParams)],
)
def test_absent_fields_keep_sdk_defaults(config_cls, params_cls):
    config = config_cls(url="https://mcp.example.com")
    params = _mcp_params(config, params_cls)
    defaults = params_cls(url=config.url)
    assert params.timeout == defaults.timeout
    assert params.sse_read_timeout == defaults.sse_read_timeout
    assert params.httpx_client_factory is defaults.httpx_client_factory


@pytest.mark.parametrize(
    "config_cls,params_cls",
    [(ADKHTTPConnectionParams, StreamableHTTPConnectionParams), (ADKSSEConnectionParams, SseConnectionParams)],
)
@pytest.mark.parametrize(
    "settings,expected",
    [
        (
            {"tls_insecure_skip_verify": True},
            {"disable_verify": True, "ca_cert_path": None, "disable_system_cas": False},
        ),
        (
            {"tls_ca_cert_path": "/etc/ca.pem", "tls_disable_system_cas": True},
            {"disable_verify": False, "ca_cert_path": "/etc/ca.pem", "disable_system_cas": True},
        ),
        (
            {"tls_insecure_skip_verify": False},
            {"disable_verify": False, "ca_cert_path": None, "disable_system_cas": False},
        ),
    ],
)
def test_tls_factory_preserves_settings_and_call_arguments(config_cls, params_cls, settings, expected):
    config = config_cls(url="https://mcp.example.com", **settings)
    context = object()
    with mock.patch("kagent.adk.types.create_ssl_context", return_value=context) as create:
        params = _mcp_params(config, params_cls)
    create.assert_called_once_with(**expected)
    auth = httpx2.BasicAuth("user", "password")
    timeout = httpx2.Timeout(60)
    with mock.patch("kagent.adk.types.httpx2.AsyncClient") as client:
        params.httpx_client_factory(headers={"X-Token": "token"}, timeout=timeout, auth=auth)
    assert client.call_args.kwargs["verify"] is context
    assert client.call_args.kwargs["timeout"] is timeout
    assert client.call_args.kwargs["auth"] is auth
    assert client.call_args.kwargs["headers"] == {"X-Token": "token"}
