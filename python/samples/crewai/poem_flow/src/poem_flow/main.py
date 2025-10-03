#!/usr/bin/env python
import json
import logging
import os
from random import randint

import uvicorn
from crewai.flow import Flow, listen, persist, start
from kagent.crewai import KAgentApp
from pydantic import BaseModel

from poem_flow.crews.poem_crew.poem_crew import PoemCrew

os.makedirs("output", exist_ok=True)

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class PoemState(BaseModel):
    sentence_count: int = 1
    poem: str = ""


# The persist decorator will persist all the flow method states to KAgent backend
# Alternatively, you can persist only certain methods by adding @persist to those methods
@persist(verbose=True)
class PoemFlow(Flow[PoemState]):
    @start()
    def generate_sentence_count(self):
        logging.info(f"Flow starting. Initial state: {self.state}")
        logging.info("Generating sentence count")
        self.state.sentence_count = randint(1, 5)

    @listen(generate_sentence_count)
    def generate_poem(self):
        logging.info("Generating poem")
        poem_crew = PoemCrew().crew()

        if self.state.poem:
            logging.info("Continuing existing poem...")
            continuation_task = poem_crew.tasks[0]
            continuation_task.description = f"""Continue this poem about how CrewAI is awesome, adding {self.state.sentence_count} more sentences.
Keep the tone funny and light-hearted. Respond only with the new lines of the poem, do not repeat the existing poem.

EXISTING POEM:
---
{self.state.poem}
---"""
            continuation_task.expected_output = f"The next {self.state.sentence_count} sentences of the poem."
            result = poem_crew.kickoff()
            self.state.poem += f"\n{result.raw}"
        else:
            logging.info("Starting a new poem...")
            result = poem_crew.kickoff(inputs={"sentence_count": self.state.sentence_count})
            self.state.poem = result.raw

        logging.info(f"Poem state is now:\n{self.state.poem}")

    @listen(generate_poem)
    def save_poem(self):
        logging.info("Saving poem")
        return self.state.poem


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
