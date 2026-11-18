import json
import logging
import os
from pathlib import Path

from kagent.core import materialize_from_bootstrap
from kagent.crewai import KAgentApp

from research_crew.crew import ResearchCrew

os.makedirs("output", exist_ok=True)

logging.basicConfig(level=logging.INFO)


def main():
    """Main entry point to run the KAgent CrewAI server."""
    materialize_from_bootstrap("/config")
    agent_card = json.loads(Path("/config/agent-card.json").read_text())
    app = KAgentApp(crew=ResearchCrew().crew(), agent_card=agent_card)

    app.build().run()


if __name__ == "__main__":
    main()
