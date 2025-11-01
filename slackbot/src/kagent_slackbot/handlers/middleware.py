"""Middleware for metrics and logging"""

import time
from typing import Any, Awaitable, Callable

from prometheus_client import Counter, Histogram
from slack_bolt.async_app import AsyncApp
from structlog import get_logger

logger = get_logger(__name__)

# Prometheus metrics with kagent_slackbot namespace prefix
slack_messages_total = Counter(
    "kagent_slackbot_messages_total",
    "Total Slack messages processed",
    ["event_type", "status"],
)

slack_message_duration_seconds = Histogram(
    "kagent_slackbot_message_duration_seconds",
    "Message processing duration",
    ["event_type"],
)

slack_commands_total = Counter(
    "kagent_slackbot_commands_total", "Total slash commands", ["command", "status"]
)

agent_invocations_total = Counter(
    "kagent_slackbot_agent_invocations_total",
    "Total agent invocations",
    ["agent", "status"],
)


def register_middleware(app: AsyncApp) -> None:
    """Register middleware for metrics and logging"""

    @app.middleware
    async def metrics_middleware(body: dict[str, Any], next_: Callable[[], Awaitable[None]]) -> None:
        """Collect metrics for all events"""

        event_type = body.get("event", {}).get("type") or body.get("type", "unknown")
        start_time = time.time()

        try:
            await next_()
            slack_messages_total.labels(event_type=event_type, status="success").inc()
        except Exception as e:
            slack_messages_total.labels(event_type=event_type, status="error").inc()
            logger.error("Middleware error", event_type=event_type, error=str(e))
            raise
        finally:
            duration = time.time() - start_time
            slack_message_duration_seconds.labels(event_type=event_type).observe(duration)
