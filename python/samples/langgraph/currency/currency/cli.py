"""CLI for the basic LangGraph agent."""

import json
import logging
import os

import uvicorn
from agent import graph
from kagent.core import KAgentConfig
from kagent.langgraph import KAgentApp

# Configure logging
logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")

logger = logging.getLogger(__name__)


def main():
    """Main entry point for the CLI."""
    # from script directory
    with open(os.path.join(os.path.dirname(__file__), "agent-card.json"), "r") as f:
        agent_card = json.load(f)

    config = KAgentConfig()
    app = KAgentApp(graph=graph, agent_card=agent_card, config=config, tracing=True)

    port = int(os.getenv("PORT", "8080"))
    host = os.getenv("HOST", "0.0.0.0")
    logger.info(f"Starting server on {host}:{port}")

    uvicorn.run(
        app.build(),
        host=host,
        port=port,
        log_level="info",
    )


if __name__ == "__main__":
    main()
