"""Compatibility wrappers for a2a-sdk HTTP route registration.

a2a-sdk 1.x removed the old ``a2a.server.apps`` helper classes. KAgent still
uses the v0.3 Pydantic model surface internally, so these wrappers keep the
local app-building code small while registering the 1.x Starlette routes with
v0.3 JSON-RPC compatibility enabled.
"""

from urllib.parse import urlparse

from a2a.compat.v0_3.conversions import to_core_agent_card
from a2a.compat.v0_3.types import AgentCard
from a2a.server.request_handlers import RequestHandler
from a2a.server.routes import create_agent_card_routes, create_jsonrpc_routes


def _route_path(url: str | None) -> str:
    parsed = urlparse(url or "/")
    return parsed.path or "/"


class A2AStarletteApplication:
    def __init__(
        self,
        *,
        agent_card: AgentCard,
        http_handler: RequestHandler,
        max_content_length: int | None = None,
    ) -> None:
        self.agent_card = agent_card
        self.http_handler = http_handler
        self.max_content_length = max_content_length

    def add_routes_to_app(self, app) -> None:
        core_agent_card = to_core_agent_card(self.agent_card)
        for route in create_agent_card_routes(core_agent_card):
            app.router.routes.append(route)
        for route in create_jsonrpc_routes(
            self.http_handler,
            rpc_url=_route_path(self.agent_card.url),
            enable_v0_3_compat=True,
        ):
            app.router.routes.append(route)


class A2AFastAPIApplication(A2AStarletteApplication):
    """Name-compatible alias for the old a2a.server.apps FastAPI helper."""

    pass
