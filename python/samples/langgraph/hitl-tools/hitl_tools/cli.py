"""CLI for the HITL tools LangGraph agent."""

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
    """Build the HITL tools ASGI application."""
    with open(os.path.join(os.path.dirname(__file__), "agent-card.json"), "r") as f:
        agent_card = json.load(f)

    return KAgentApp(
        graph=graph,
        agent_card=agent_card,
        config=KAgentConfig(),
        tracing=True,
    ).build()


def main():
    """Main entry point for the CLI."""
    app = build_app()

    port = int(os.getenv("PORT", "8080"))
    host = os.getenv("HOST", "0.0.0.0")
    logger.info(f"Starting server on {host}:{port}")

    uvicorn.run(
        app,
        host=host,
        port=port,
        log_level="info",
    )


if __name__ == "__main__":
    main()
