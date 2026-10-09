"""CLI for the LangGraph kebab sample."""

import json
import logging
import os

import uvicorn
from kagent.core import KAgentConfig
from kagent.langgraph import KAgentApp

from .agent import graph

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")

logger = logging.getLogger(__name__)


def build_app():
    """Build the kebab ASGI application."""
    with open(os.path.join(os.path.dirname(__file__), "agent-card.json"), "r") as f:
        agent_card = json.load(f)

    return KAgentApp(
        graph=graph,
        agent_card=agent_card,
        config=KAgentConfig(),
        tracing=False,
    ).build()


def main():
    """Run the kebab agent server."""
    app = build_app()

    port = int(os.getenv("PORT", "8080"))
    host = os.getenv("HOST", "0.0.0.0")
    logger.info("Starting server on %s:%s", host, port)

    uvicorn.run(
        app,
        host=host,
        port=port,
        log_level="info",
    )


if __name__ == "__main__":
    main()
