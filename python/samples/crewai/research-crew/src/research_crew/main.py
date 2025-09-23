import json
import logging
import os

import uvicorn
from kagent.crewai import KAgentApp

from research_crew.crew import ResearchCrew

os.makedirs("output", exist_ok=True)

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def main():
    """Main entry point to run the KAgent CrewAI server."""
    # 1. Load the agent card or define it inline
    with open(os.path.join(os.path.dirname(__file__), "agent-card.json"), "r") as f:
        agent_card = json.load(f)

    # 2. Load the Crew, then create the kagent app
    app = KAgentApp(crew=ResearchCrew().crew(), agent_card=agent_card)

    # 3. Build the FastAPI app and run the server
    server = app.build()
    port = int(os.getenv("PORT", "8080"))
    host = os.getenv("HOST", "0.0.0.0")
    logger.info(f"Starting server on {host}:{port}")
    uvicorn.run(
        server,
        host=host,
        port=port,
        log_level="info",
    )


if __name__ == "__main__":
    main()
