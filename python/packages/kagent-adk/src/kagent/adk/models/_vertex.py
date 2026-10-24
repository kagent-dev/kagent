"""Vertex AI authentication through the Substrate egress gateway."""

from __future__ import annotations

import os
import re
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


def claude_vertex_llm(model: str) -> ClaudeLLM:
    """Claude on Vertex AI. Under KAGENT_SKIP_VERTEX_AUTH its client sends the
    gateway placeholder bearer instead of resolving Google credentials.

    The project and location come from the environment, or from a model given
    as a full ``projects/<project>/locations/<location>/...`` resource name.
    """
    if not skip_vertex_auth():
        return ClaudeLLM(model=model)
    project_id = os.environ.get("GOOGLE_CLOUD_PROJECT")
    location = os.environ.get("GOOGLE_CLOUD_LOCATION")
    match = re.search(r"projects/([^/]+)/locations/([^/]+)/", model)
    if match:
        project_id, location = match.group(1), match.group(2)
    if not project_id or not location:
        raise ValueError(f"Claude model {model!r} on Vertex AI needs GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION")
    client = AsyncAnthropicVertex(project_id=project_id, region=location, access_token=VERTEX_GATEWAY_TOKEN)
    return ClaudeLLM(model=model, client=client)
