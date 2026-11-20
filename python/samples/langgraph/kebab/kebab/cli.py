"""CLI for the LangGraph kebab sample."""

import json
import logging
from pathlib import Path

from agent import graph
from kagent.core import KAgentConfig, materialize_from_bootstrap
from kagent.langgraph import KAgentApp

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")


def main():
    materialize_from_bootstrap("/config")
    agent_card = json.loads(Path("/config/agent-card.json").read_text())
    config = KAgentConfig.from_file()
    app = KAgentApp(
        graph=graph,
        agent_card=agent_card,
        config=config,
        tracing=False,
    )

    app.build().run()


if __name__ == "__main__":
    main()
