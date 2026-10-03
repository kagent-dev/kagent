from unittest import mock

import httpx
import pytest
from openai.lib.azure import API_KEY_SENTINEL

from kagent.adk.models._azure import build_azure_openai_client

_SENTINEL_TOKEN_PROVIDER = object()


def test_azure_openai_workload_identity_when_no_api_key():
    from kagent.adk.models import AzureOpenAI

    with (
        mock.patch.dict("os.environ", {"AZURE_OPENAI_ENDPOINT": "https://test.openai.azure.com"}, clear=True),
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI") as mock_azure,
        mock.patch(
            "kagent.adk.models._azure.azure_ad_token_provider", return_value=_SENTINEL_TOKEN_PROVIDER
        ) as mock_provider,
    ):
        llm = AzureOpenAI(model="gpt-4o", type="azure_openai")
        _ = llm._client

        mock_provider.assert_called_once()
        kwargs = mock_azure.call_args.kwargs
        assert kwargs["azure_ad_token_provider"] is _SENTINEL_TOKEN_PROVIDER
        assert kwargs["api_key"] == API_KEY_SENTINEL


def test_azure_openai_api_key_skips_workload_identity():
    from kagent.adk.models import AzureOpenAI

    with (
        mock.patch.dict(
            "os.environ",
            {"AZURE_OPENAI_ENDPOINT": "https://test.openai.azure.com", "AZURE_OPENAI_API_KEY": "secret"},
            clear=True,
        ),
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI") as mock_azure,
        mock.patch("kagent.adk.models._azure.azure_ad_token_provider") as mock_provider,
    ):
        llm = AzureOpenAI(model="gpt-4o", type="azure_openai")
        _ = llm._client

        mock_provider.assert_not_called()
        kwargs = mock_azure.call_args.kwargs
        assert kwargs["api_key"] == "secret"
        assert kwargs["azure_ad_token_provider"] is None


def test_azure_openai_passthrough_does_not_fall_back_to_workload_identity():
    from kagent.adk.models import AzureOpenAI

    with (
        mock.patch.dict("os.environ", {"AZURE_OPENAI_ENDPOINT": "https://test.openai.azure.com"}, clear=True),
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI"),
        mock.patch("kagent.adk.models._azure.azure_ad_token_provider") as mock_provider,
    ):
        llm = AzureOpenAI(model="gpt-4o", type="azure_openai", api_key_passthrough=True)
        with pytest.raises(ValueError, match="No Azure credential resolved"):
            _ = llm._client
        mock_provider.assert_not_called()


def test_foundry_workload_identity_when_no_api_key():
    from kagent.adk.models import Foundry

    with (
        mock.patch.dict(
            "os.environ",
            {
                "FOUNDRY_ENDPOINT": "https://test.cognitiveservices.azure.com/",
                "FOUNDRY_DEPLOYMENT": "gpt-4o",
            },
            clear=True,
        ),
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI") as mock_azure,
        mock.patch(
            "kagent.adk.models._azure.azure_ad_token_provider", return_value=_SENTINEL_TOKEN_PROVIDER
        ) as mock_provider,
    ):
        llm = Foundry(model="gpt-4o", type="foundry")
        _ = llm._client

        mock_provider.assert_called_once()
        kwargs = mock_azure.call_args.kwargs
        assert kwargs["azure_ad_token_provider"] is _SENTINEL_TOKEN_PROVIDER
        assert kwargs["api_key"] == API_KEY_SENTINEL
        assert kwargs["azure_endpoint"] == "https://test.cognitiveservices.azure.com/"
        assert kwargs["azure_deployment"] == "gpt-4o"
        assert kwargs["api_version"] == "2024-10-21"


@pytest.mark.asyncio
async def test_azure_client_workload_identity_uses_bearer_with_real_sdk():
    seen_request: httpx.Request | None = None

    async def token_provider() -> str:
        return "workload-token"

    async def handler(request: httpx.Request) -> httpx.Response:
        nonlocal seen_request
        seen_request = request
        return httpx.Response(
            200,
            json={
                "id": "chatcmpl-test",
                "object": "chat.completion",
                "created": 0,
                "model": "gpt-4o",
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": "ok"},
                        "finish_reason": "stop",
                    }
                ],
            },
        )

    http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    with (
        mock.patch.dict("os.environ", {"AZURE_OPENAI_API_KEY": "must-not-win"}, clear=True),
        mock.patch("kagent.adk.models._azure.azure_ad_token_provider", return_value=token_provider),
    ):
        client = build_azure_openai_client(
            api_version="2024-10-21",
            azure_endpoint="https://test.cognitiveservices.azure.com/",
            azure_deployment="gpt-4o",
            api_key=None,
            api_key_passthrough=False,
            default_headers=None,
            http_client=http_client,
            missing_credential_hint="missing credential",
        )

    try:
        response = await client.chat.completions.create(
            model="gpt-4o",
            messages=[{"role": "user", "content": "hello"}],
        )
    finally:
        await client.close()

    assert response.choices[0].message.content == "ok"
    assert seen_request is not None
    assert seen_request.headers["authorization"] == "Bearer workload-token"
    assert "api-key" not in seen_request.headers


def test_foundry_api_key_from_env():
    from kagent.adk.models import Foundry

    with (
        mock.patch.dict(
            "os.environ",
            {
                "FOUNDRY_ENDPOINT": "https://test.cognitiveservices.azure.com/",
                "FOUNDRY_DEPLOYMENT": "gpt-4o",
                "FOUNDRY_API_VERSION": "2025-01-01",
                "FOUNDRY_API_KEY": "secret",
            },
            clear=True,
        ),
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI") as mock_azure,
        mock.patch("kagent.adk.models._azure.azure_ad_token_provider") as mock_provider,
    ):
        llm = Foundry(model="gpt-4o", type="foundry")
        _ = llm._client

        mock_provider.assert_not_called()
        kwargs = mock_azure.call_args.kwargs
        assert kwargs["api_key"] == "secret"
        assert kwargs["azure_ad_token_provider"] is None
        assert kwargs["api_version"] == "2025-01-01"


def test_foundry_sanitizes_default_auth_headers():
    from kagent.adk.models import Foundry

    with (
        mock.patch.dict(
            "os.environ",
            {
                "FOUNDRY_ENDPOINT": "https://test.cognitiveservices.azure.com/",
                "FOUNDRY_DEPLOYMENT": "gpt-4o",
                "FOUNDRY_API_KEY": "must-not-win",
            },
            clear=True,
        ),
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI") as mock_azure,
        mock.patch(
            "kagent.adk.models._azure.azure_ad_token_provider",
            return_value=_SENTINEL_TOKEN_PROVIDER,
        ),
    ):
        llm = Foundry(
            model="gpt-4o",
            type="foundry",
            default_headers={
                "Authorization": "Bearer leaked",
                "api-key": "leaked",
                "X-Custom": "preserved",
            },
        )
        _ = llm._client

        assert mock_azure.call_args.kwargs["default_headers"] == {"X-Custom": "preserved"}


def test_foundry_passthrough_uses_caller_token():
    from kagent.adk.models import Foundry

    with (
        mock.patch.dict(
            "os.environ",
            {"FOUNDRY_ENDPOINT": "https://test.cognitiveservices.azure.com/", "FOUNDRY_DEPLOYMENT": "gpt-4o"},
            clear=True,
        ),
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI") as mock_azure,
        mock.patch("kagent.adk.models._azure.azure_ad_token_provider") as mock_provider,
    ):
        llm = Foundry(model="gpt-4o", type="foundry", api_key_passthrough=True)
        llm.set_passthrough_key("caller-token")
        _ = llm._client

        mock_provider.assert_not_called()
        assert mock_azure.call_args.kwargs["api_key"] == "caller-token"


def test_foundry_passthrough_without_token_does_not_use_workload_identity():
    from kagent.adk.models import Foundry

    with (
        mock.patch.dict(
            "os.environ",
            {"FOUNDRY_ENDPOINT": "https://test.cognitiveservices.azure.com/", "FOUNDRY_DEPLOYMENT": "gpt-4o"},
            clear=True,
        ),
        mock.patch("kagent.adk.models._azure.azure_ad_token_provider") as mock_provider,
    ):
        llm = Foundry(model="gpt-4o", type="foundry", api_key_passthrough=True)
        with pytest.raises(ValueError, match="No Azure credential resolved"):
            _ = llm._client

        mock_provider.assert_not_called()


def test_foundry_client_with_tls():
    import ssl

    from kagent.adk.models import Foundry

    with (
        mock.patch("kagent.adk.models._ssl.create_ssl_context") as mock_create_ssl,
        mock.patch("kagent.adk.models._openai.DefaultAsyncHttpxClient") as mock_httpx,
        mock.patch("kagent.adk.models._azure.AsyncAzureOpenAI") as mock_azure,
    ):
        mock_ssl_context = mock.MagicMock(spec=ssl.SSLContext)
        mock_create_ssl.return_value = mock_ssl_context
        mock_httpx_instance = mock.MagicMock()
        mock_httpx.return_value = mock_httpx_instance

        llm = Foundry(
            model="gpt-4o",
            type="foundry",
            endpoint="https://test.cognitiveservices.azure.com/",
            deployment="gpt-4o",
            api_key="foundry-key",
            tls_ca_cert_path="/etc/ssl/certs/custom/corp-ca/ca.crt",
        )
        _ = llm._client

        assert mock_httpx.call_args.kwargs["verify"] is mock_ssl_context
        assert mock_azure.call_args.kwargs["http_client"] is mock_httpx_instance


def test_foundry_missing_endpoint_raises():
    from kagent.adk.models import Foundry

    with mock.patch.dict("os.environ", {"FOUNDRY_DEPLOYMENT": "gpt-4o"}, clear=True):
        llm = Foundry(model="gpt-4o", type="foundry")
        with pytest.raises(ValueError, match="Foundry endpoint must be provided"):
            _ = llm._client


def test_foundry_missing_deployment_raises():
    from kagent.adk.models import Foundry

    with mock.patch.dict("os.environ", {"FOUNDRY_ENDPOINT": "https://test.cognitiveservices.azure.com/"}, clear=True):
        llm = Foundry(model="gpt-4o", type="foundry")
        with pytest.raises(ValueError, match="Foundry deployment must be provided"):
            _ = llm._client
