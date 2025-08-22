"""CLI for the basic LangGraph agent."""

import asyncio
import json
import logging
import os
import sys

import uvicorn
from agent import CurrencyAgent
from kagent.core import KAgentConfig
from kagent.langgraph import KAgentApp
# from .agent import root_app

# Configure logging
logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")

logger = logging.getLogger(__name__)


def main():
    """Main entry point for the CLI."""
    # from script directory
    with open(os.path.join(os.path.dirname(__file__), "agent-card.json"), "r") as f:
        agent_card = json.load(f)

    config = KAgentConfig()
    agent = CurrencyAgent()
    app = KAgentApp(
        graph=agent.graph,
        agent_card=agent_card,
        kagent_url=config.url,
        app_name=config.app_name,
    )

    port = int(os.getenv("PORT", "8080"))
    host = os.getenv("HOST", "0.0.0.0")
    logger.info(f"Starting server on {host}:{port}")

    uvicorn.run(
        app.build(),
        host=host,
        port=port,
        log_level="info",
    )


# def run_server():
#     """Run the FastAPI server."""
#     app = root_app.build()

#     port = int(os.getenv("PORT", "8080"))
#     host = os.getenv("HOST", "0.0.0.0")

#     logger.info(f"Starting server on {host}:{port}")

#     uvicorn.run(
#         app,
#         host=host,
#         port=port,
#         log_level="info",
#     )


# async def test_agent():
#     """Test the agent with a simple query."""
#     logger.info("Testing basic LangGraph agent...")

#     try:
#         await root_app.test(task="Hello! Can you tell me a short joke?", session_id="test-session-123")
#     except Exception as e:
#         logger.error(f"Test failed: {e}", exc_info=True)
#         sys.exit(1)


if __name__ == "__main__":
    main()
