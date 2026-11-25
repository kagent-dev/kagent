"""CLI for the HITL tools LangGraph agent."""

import json
import logging
from pathlib import Path

from agent import graph
from kagent.core import KAgentConfig, materialize_from_bootstrap
from kagent.langgraph import KAgentApp

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")


def main():
    """Main entry point for the CLI."""
    materialize_from_bootstrap("/config")
    agent_card = json.loads(Path("/config/agent-card.json").read_text())
    config = KAgentConfig.from_file()
    app = KAgentApp(
        graph=graph,
        agent_card=agent_card,
        config=config,
        tracing=True,
    )

    app.build().run()


if __name__ == "__main__":
    main()
