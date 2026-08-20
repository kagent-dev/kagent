"""Shared configuration and client helpers for Azure AI providers."""

from __future__ import annotations

import os
from typing import TYPE_CHECKING, Any, Awaitable, Callable, Optional

import httpx
from anthropic import AsyncAnthropic
from openai import AsyncAzureOpenAI
from openai.lib.azure import API_KEY_SENTINEL

if TYPE_CHECKING:
    from anthropic.lib.credentials import AccessToken, AccessTokenProvider
    from azure.identity import DefaultAzureCredential

COGNITIVE_SERVICES_SCOPE = "https://cognitiveservices.azure.com/.default"
AI_FOUNDRY_SCOPE = "https://ai.azure.com/.default"

AZURE_OPENAI_DEFAULT_API_VERSION = "2024-02-15-preview"
FOUNDRY_DEFAULT_API_VERSION = "2024-10-21"

_AUTH_HEADER_NAMES = {"authorization", "api-key", "x-api-key"}

AsyncTokenProvider = Callable[[], Awaitable[str]]


def azure_ad_token_provider(scope: str = COGNITIVE_SERVICES_SCOPE) -> AsyncTokenProvider:
    """Return an async bearer-token provider backed by ``DefaultAzureCredential``."""
    from azure.identity.aio import DefaultAzureCredential, get_bearer_token_provider

    return get_bearer_token_provider(DefaultAzureCredential(), scope)


class _AzureAccessTokenProvider:
    """Adapt Azure Identity to Anthropic's synchronous access-token provider."""

    def __init__(self, scope: str) -> None:
        from azure.identity import DefaultAzureCredential

        self._credential: DefaultAzureCredential = DefaultAzureCredential()
        self._scope = scope

    def __call__(self, *, force_refresh: bool = False) -> "AccessToken":
        del force_refresh

        from anthropic.lib.credentials import AccessToken

        token = self._credential.get_token(self._scope)
        return AccessToken(token=token.token, expires_at=token.expires_on)

    def close(self) -> None:
        self._credential.close()


def azure_access_token_provider(scope: str = AI_FOUNDRY_SCOPE) -> "AccessTokenProvider":
    """Return an Azure provider compatible with Anthropic's token cache."""
    return _AzureAccessTokenProvider(scope)


def resolve_azure_api_key(
    api_key: Optional[str],
    *,
    api_key_passthrough: Optional[bool],
    environment_variable: str,
) -> Optional[str]:
    """Resolve an Azure API key without bypassing passthrough mode."""
    if api_key_passthrough:
        return api_key
    return api_key or os.environ.get(environment_variable)


def resolve_azure_openai_config(endpoint: Optional[str], api_version: Optional[str]) -> tuple[str, str]:
    """Resolve Azure OpenAI endpoint and API version configuration."""
    resolved_endpoint = endpoint or os.environ.get("AZURE_OPENAI_ENDPOINT")
    if not resolved_endpoint:
        raise ValueError(
            "Azure endpoint must be provided either via azure_endpoint parameter or "
            "AZURE_OPENAI_ENDPOINT environment variable"
        )

    resolved_api_version = api_version or os.environ.get("OPENAI_API_VERSION") or AZURE_OPENAI_DEFAULT_API_VERSION
    return resolved_endpoint, resolved_api_version


def resolve_foundry_endpoint_deployment(endpoint: Optional[str], deployment: Optional[str]) -> tuple[str, str]:
    """Resolve the Foundry endpoint and deployment."""
    resolved_endpoint = endpoint or os.environ.get("FOUNDRY_ENDPOINT")
    if not resolved_endpoint:
        raise ValueError(
            "Foundry endpoint must be provided either via endpoint parameter or FOUNDRY_ENDPOINT environment variable"
        )

    resolved_deployment = deployment or os.environ.get("FOUNDRY_DEPLOYMENT")
    if not resolved_deployment:
        raise ValueError(
            "Foundry deployment must be provided either via deployment parameter or "
            "FOUNDRY_DEPLOYMENT environment variable"
        )

    return resolved_endpoint, resolved_deployment


def resolve_foundry_config(
    endpoint: Optional[str], deployment: Optional[str], api_version: Optional[str]
) -> tuple[str, str, str]:
    """Resolve Foundry OpenAI-compatible data-plane configuration."""
    resolved_endpoint, resolved_deployment = resolve_foundry_endpoint_deployment(endpoint, deployment)

    resolved_api_version = api_version or os.environ.get("FOUNDRY_API_VERSION") or FOUNDRY_DEFAULT_API_VERSION
    return resolved_endpoint, resolved_deployment, resolved_api_version


def sanitize_azure_auth_headers(headers: Optional[dict[str, str]]) -> Optional[dict[str, str]]:
    """Remove configured headers that could conflict with resolved Azure auth."""
    if not headers:
        return None
    sanitized = {name: value for name, value in headers.items() if name.lower() not in _AUTH_HEADER_NAMES}
    return sanitized or None


def build_azure_openai_client(
    *,
    api_version: str,
    azure_endpoint: str,
    azure_deployment: Optional[str],
    api_key: Optional[str],
    api_key_passthrough: Optional[bool],
    default_headers: Optional[dict[str, str]],
    http_client: Optional[httpx.AsyncClient],
    missing_credential_hint: str,
) -> AsyncAzureOpenAI:
    """Build an Azure OpenAI client using key, passthrough, or Workload Identity auth."""
    token_provider = None
    if not api_key:
        if api_key_passthrough:
            raise ValueError(missing_credential_hint)
        token_provider = azure_ad_token_provider()

    return AsyncAzureOpenAI(
        # The sentinel prevents environment-key fallback while the token
        # provider authenticates each request.
        api_key=API_KEY_SENTINEL if token_provider is not None else api_key,
        azure_ad_token_provider=token_provider,
        api_version=api_version,
        azure_endpoint=azure_endpoint,
        azure_deployment=azure_deployment,
        default_headers=sanitize_azure_auth_headers(default_headers),
        http_client=http_client,
    )


def build_foundry_anthropic_client(
    *,
    endpoint: str,
    api_key: Optional[str],
    api_key_passthrough: Optional[bool],
    default_headers: Optional[dict[str, str]],
    http_client: Optional[httpx.AsyncClient],
) -> AsyncAnthropic:
    """Build a Foundry Anthropic client using key, passthrough, or Workload Identity auth."""
    if api_key_passthrough and not api_key:
        raise ValueError(
            "No Azure credential resolved: provide the passthrough token before creating the Foundry Anthropic client"
        )

    kwargs: dict[str, Any] = {"base_url": endpoint.rstrip("/") + "/anthropic"}
    if api_key:
        kwargs["api_key"] = api_key
    else:
        kwargs["credentials"] = azure_access_token_provider(AI_FOUNDRY_SCOPE)

    safe_headers = sanitize_azure_auth_headers(default_headers)
    if safe_headers:
        kwargs["default_headers"] = safe_headers
    if http_client is not None:
        kwargs["http_client"] = http_client

    return AsyncAnthropic(**kwargs)
