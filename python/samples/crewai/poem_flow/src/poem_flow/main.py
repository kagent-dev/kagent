#!/usr/bin/env python
import json
import logging
import os
from random import randint

import uvicorn
from crewai.flow import Flow, listen, start
from kagent.crewai import KAgentApp
from pydantic import BaseModel

from poem_flow.crews.poem_crew.poem_crew import PoemCrew

os.makedirs("output", exist_ok=True)

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class PoemState(BaseModel):
    sentence_count: int = 1
    poem: str = ""


class PoemFlow(Flow[PoemState]):
    @start()
    def generate_sentence_count(self):
        logging.info("Generating sentence count")
        self.state.sentence_count = randint(1, 5)

    @listen(generate_sentence_count)
    def generate_poem(self):
        logging.info("Generating poem")
        result = PoemCrew().crew().kickoff(inputs={"sentence_count": self.state.sentence_count})

        logging.info("Poem generated", result.raw)
        self.state.poem = result.raw

    @listen(generate_poem)
    def save_poem(self):
        logging.info("Saving poem")
        with open("output/poem.txt", "w") as f:
            f.write(self.state.poem)


# These two methods are for the script that crewai CLI uses
def kickoff():
    poem_flow = PoemFlow()
    poem_flow.kickoff()


def plot():
    poem_flow = PoemFlow()
    poem_flow.plot()


# To integrate with Kagent, just replace the kickoff above with the KAgentApp code below
def main():
    """Main entry point to run the KAgent CrewAI server."""
    with open(os.path.join(os.path.dirname(__file__), "agent-card.json"), "r") as f:
        agent_card = json.load(f)

    app = KAgentApp(crew=PoemFlow(), agent_card=agent_card)

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
