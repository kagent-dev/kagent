"""LLM pass-through header plugin.

Forwards caller-supplied headers from the incoming A2A request onto the
outbound LLM call, for the header names listed in the model's
``passthrough_headers`` config. Incoming request headers are read from
``callback_context.state["headers"]`` (set by ``_agent_executor.py``), the
same pattern as ``create_header_provider()`` and ``LLMPassthroughPlugin``.

Delivery is provider-specific: models exposing ``set_passthrough_headers``
(Anthropic) receive the resolved headers directly, because their SDK call
happens inside the ADK base class where the outbound request cannot be
modified per call; all other models receive them on the per-request
``llm_request.config.http_options.headers``.
"""

import logging
from typing import Optional

from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.adk.plugins.base_plugin import BasePlugin
from google.genai import types as genai_types

logger = logging.getLogger(__name__)

# Credential headers (Authorization, Proxy-Authorization, Cookie) plus the
# RFC 9110 hop-by-hop / message-framing set. Must stay in sync with
# restrictedPassthroughHeaders in go/adk/pkg/headers/headers.go, which explains
# each group.
RESTRICTED_PASSTHROUGH_HEADERS = frozenset(
    {
        "authorization",
        "connection",
        "content-length",
        "cookie",
        "host",
        "keep-alive",
        "proxy-authenticate",
        "proxy-authorization",
        "proxy-connection",
        "te",
        "trailer",
        "transfer-encoding",
        "upgrade",
    }
)


def _resolve_passthrough_headers(allowed: list[str], request_headers: dict[str, str]) -> dict[str, str]:
    """Return the allowed subset of the incoming request headers.

    Matching is case-insensitive on both sides; keys in the result preserve
    the casing from the configured list. Headers absent from the request, or
    present with an empty value, are omitted rather than sent empty.
    """
    lookup = {k.lower(): v for k, v in request_headers.items()}
    resolved: dict[str, str] = {}
    for name in allowed:
        lname = name.lower()
        if lname in RESTRICTED_PASSTHROUGH_HEADERS:
            continue
        value = lookup.get(lname)
        if value:
            resolved[name] = value
    return resolved


class LLMHeaderPassthroughPlugin(BasePlugin):
    """Forwards configured pass-through headers from the incoming request to the LLM call."""

    def __init__(self):
        super().__init__(name="llm_header_passthrough")

    async def before_model_callback(
        self, *, callback_context: CallbackContext, llm_request: LlmRequest
    ) -> Optional[LlmResponse]:
        model = callback_context._invocation_context.agent.model
        allowed = getattr(model, "passthrough_headers", None)
        if not allowed:
            return None

        request_headers = callback_context.state.get("headers", {})
        resolved = _resolve_passthrough_headers(allowed, request_headers)

        setter = getattr(model, "set_passthrough_headers", None)
        if callable(setter):
            # resolved may be empty and must still reach the setter: the model is
            # shared across requests, so an empty result must clear the previous
            # caller's values rather than inherit them.
            setter(resolved)
            return None

        if not resolved:
            return None

        config = llm_request.config
        if config is None:
            config = genai_types.GenerateContentConfig()
            llm_request.config = config
        http_options = config.http_options
        if http_options is None:
            http_options = genai_types.HttpOptions()
            config.http_options = http_options
        merged = dict(http_options.headers or {})
        merged.update(resolved)
        http_options.headers = merged
        logger.debug("Forwarding %d pass-through header(s) to the LLM call", len(resolved))
        return None
