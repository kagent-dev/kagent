"""Main application entry point"""

import asyncio

import structlog
from aiohttp import web
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest
from slack_bolt.adapter.socket_mode.async_handler import AsyncSocketModeHandler
from slack_bolt.async_app import AsyncApp

from .auth.permissions import PermissionChecker
from .auth.slack_groups import SlackGroupChecker
from .config import load_config
from .constants import AGENT_CACHE_TTL
from .handlers.actions import register_action_handlers
from .handlers.commands import register_command_handlers
from .handlers.mentions import register_mention_handlers
from .handlers.middleware import register_middleware
from .services.a2a_client import A2AClient
from .services.agent_discovery import AgentDiscovery
from .services.agent_router import AgentRouter

# Configure structured logging
structlog.configure(
    processors=[
        structlog.stdlib.filter_by_level,
        structlog.stdlib.add_logger_name,
        structlog.stdlib.add_log_level,
        structlog.stdlib.PositionalArgumentsFormatter(),
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
        structlog.processors.UnicodeDecoder(),
        structlog.processors.JSONRenderer(),
    ],
    context_class=dict,
    logger_factory=structlog.stdlib.LoggerFactory(),
    cache_logger_on_first_use=True,
)

logger = structlog.get_logger(__name__)


async def health_check(request: web.Request) -> web.Response:
    """Health check endpoint"""
    return web.Response(text="OK")


async def metrics_endpoint(request: web.Request) -> web.Response:
    """Prometheus metrics endpoint"""
    return web.Response(
        body=generate_latest(),
        content_type=CONTENT_TYPE_LATEST,
    )


async def start_health_server(host: str, port: int) -> None:
    """Start health check HTTP server"""
    app = web.Application()
    app.router.add_get("/health", health_check)
    app.router.add_get("/ready", health_check)
    app.router.add_get("/metrics", metrics_endpoint)

    runner = web.AppRunner(app)
    await runner.setup()

    site = web.TCPSite(runner, host, port)
    await site.start()

    logger.info("Health server started", host=host, port=port)


async def main() -> None:
    """Main application"""

    # Load configuration
    config = load_config()

    logger.info(
        "Starting Kagent Slackbot",
        log_level=config.log_level,
        kagent_url=config.kagent.base_url,
    )

    # Initialize services
    a2a_client = A2AClient(
        base_url=config.kagent.base_url,
        timeout=config.kagent.timeout,
    )

    agent_discovery = AgentDiscovery(
        base_url=config.kagent.base_url,
        timeout=config.kagent.timeout,
    )

    agent_router = AgentRouter(agent_discovery)

    # Initialize Slack app
    app = AsyncApp(token=config.slack.bot_token)

    # Initialize RBAC components
    slack_group_checker = SlackGroupChecker(
        client=app.client,
        cache_ttl=AGENT_CACHE_TTL,
    )

    permission_checker = PermissionChecker(
        config_path=config.permissions_file,
        group_checker=slack_group_checker,
    )

    # Register middleware
    register_middleware(app)

    # Register handlers
    register_mention_handlers(app, a2a_client, agent_router, agent_discovery, permission_checker)
    register_command_handlers(app, agent_discovery, agent_router, permission_checker)
    register_action_handlers(app, a2a_client)

    # Start health server
    await start_health_server(config.server.host, config.server.port)

    # Start Socket Mode handler
    handler = AsyncSocketModeHandler(app, config.slack.app_token)

    logger.info("Connecting to Slack via Socket Mode")

    try:
        await handler.start_async()  # type: ignore[no-untyped-call]
    except KeyboardInterrupt:
        logger.info("Shutting down gracefully")
    finally:
        await a2a_client.close()
        await agent_discovery.close()


if __name__ == "__main__":
    asyncio.run(main())
