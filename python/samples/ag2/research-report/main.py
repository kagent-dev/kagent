"""AG2 (formerly AutoGen) research report agent for kagent.

Two agents (researcher + analyst) collaborate via GroupChat to
produce structured reports, exposed as a Kubernetes-native
agent via the A2A protocol.
"""

import json
import os

import uvicorn
from autogen import ConversableAgent, LLMConfig
from autogen.agentchat.group.patterns import AutoPattern

from kagent.ag2 import KAgentApp

llm_config = LLMConfig(
    {
        "model": os.getenv("MODEL_NAME", "gpt-4o-mini"),
        "api_key": os.environ["OPENAI_API_KEY"],
    }
)


def create_pattern():
    """Create a fresh AG2 pattern for each request."""
    researcher = ConversableAgent(
        name="researcher",
        system_message=(
            "You are a research specialist. Investigate the "
            "given topic thoroughly. Present key facts and "
            "data in a structured format. Be concise."
        ),
        llm_config=llm_config,
    )

    analyst = ConversableAgent(
        name="analyst",
        system_message=(
            "You are an analyst. Review the researcher's "
            "findings and produce a structured report with: "
            "Summary, Key Findings, Analysis, and "
            "Recommendations. Keep it under 500 words. "
            "End with TERMINATE when done."
        ),
        llm_config=llm_config,
    )

    user = ConversableAgent(
        name="user", human_input_mode="NEVER"
    )

    return AutoPattern(
        initial_agent=researcher,
        agents=[researcher, analyst],
        user_agent=user,
        group_manager_args={"llm_config": llm_config},
    )


def main():
    host = os.getenv("HOST", "0.0.0.0")
    port = int(os.getenv("PORT", "8080"))

    with open("agent-card.json") as f:
        agent_card = json.load(f)

    app = KAgentApp(
        pattern_factory=create_pattern,
        agent_card=agent_card,
        max_rounds=10,
    )
    server = app.build()
    uvicorn.run(server, host=host, port=port)


if __name__ == "__main__":
    main()
