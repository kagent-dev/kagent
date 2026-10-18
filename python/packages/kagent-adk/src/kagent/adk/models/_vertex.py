"""Vertex AI authentication through the Substrate egress gateway."""

from __future__ import annotations

import os
from functools import cached_property
from typing import Any, Optional

from anthropic import AsyncAnthropicVertex
from google.adk.models.anthropic_llm import Claude as ClaudeLLM
from google.oauth2.credentials import Credentials

SKIP_VERTEX_AUTH_ENV = "KAGENT_SKIP_VERTEX_AUTH"

# The egress gateway replaces the Authorization header a request carries with
# the access token its credential provider serves and adds none, so a call
# without this placeholder leaves unauthenticated.
VERTEX_GATEWAY_TOKEN = "kagent-credential-injected"


def skip_vertex_auth() -> bool:
    """Reports whether the egress gateway sets the Vertex AI access token."""
    return os.environ.get(SKIP_VERTEX_AUTH_ENV, "").strip().lower() in ("1", "t", "true")


def vertex_client_kwargs() -> Optional[dict[str, Any]]:
    """google-genai Client arguments for Vertex AI under KAGENT_SKIP_VERTEX_AUTH.

    A token without an expiry is never refreshed, so the client looks for no
    Application Default Credentials and sends the placeholder bearer.
    """
    if not skip_vertex_auth():
        return None
    return {"credentials": Credentials(token=VERTEX_GATEWAY_TOKEN)}


class KAgentClaudeVertexLlm(ClaudeLLM):
    """Claude on Vertex AI that sends the gateway placeholder bearer under
    KAGENT_SKIP_VERTEX_AUTH instead of resolving Google credentials."""

    @cached_property
    def _anthropic_client(self) -> AsyncAnthropicVertex:
        client = ClaudeLLM._anthropic_client.func(self)
        if skip_vertex_auth() and self.client is None:
            client.access_token = VERTEX_GATEWAY_TOKEN
        return client
