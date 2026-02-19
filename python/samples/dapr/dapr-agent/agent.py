import json
import logging
import os
import uvicorn
from dapr_agents import DurableAgent
from kagent.core import KAgentConfig
from kagent.dapr import KAgentApp

logging.basicConfig(level=logging.INFO)

def main():
    agent = DurableAgent(name="my-agent", role="assistant", instructions=["Be helpful."])
    with open(os.path.join(os.path.dirname(__file__), "agent-card.json")) as f:
        agent_card = json.load(f)
    config = KAgentConfig()
    app = KAgentApp(agent=agent, agent_card=agent_card, config=config)
    uvicorn.run(app.build(), host="0.0.0.0", port=int(os.getenv("PORT", "8080")))

if __name__ == "__main__":
    main()