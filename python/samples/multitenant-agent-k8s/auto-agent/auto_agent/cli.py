"""CLI entry point for auto-agent."""
from __future__ import annotations

import json
import logging
import os

import uvicorn
from auto_agent.agent import graph
from kagent.core import KAgentConfig
from kagent.langgraph import KAgentApp

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(name)s %(levelname)s %(message)s",
)
log = logging.getLogger(__name__)

AGENT_CARD_PATH = os.path.join(os.path.dirname(__file__), "agent-card.json")


def main() -> None:
    with open(AGENT_CARD_PATH) as f:
        agent_card = json.load(f)

    config = KAgentConfig()
    app = KAgentApp(graph=graph, agent_card=agent_card, config=config, tracing=False)

    port = int(os.getenv("PORT", "8080"))
    host = os.getenv("HOST", "0.0.0.0")
    log.info(
        "Starting auto-agent on %s:%s  tenant=%s org=%s",
        host, port,
        os.getenv("TENANT_ID", "?"),
        os.getenv("ORG_ID", "?"),
    )
    uvicorn.run(app.build(), host=host, port=port, log_level="info")


if __name__ == "__main__":
    main()
