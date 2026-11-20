"""Basic OpenAI Agent with KAgent integration.

This sample demonstrates how to create a simple OpenAI agent that can:
- Answer questions
- Use tools (calculate, get weather)
- Use skills from the skills directory
- Maintain conversation history via sessions
"""

import json
import logging
from pathlib import Path

from agents.agent import Agent
from agents.tool import function_tool
from kagent.core import KAgentConfig, materialize_from_bootstrap
from kagent.openai import KAgentApp

SKILLS_DIR = Path(__file__).parent.parent / "skills"


# Define tools for the agent
@function_tool
def calculate(expression: str) -> str:
    """Evaluate a mathematical expression and return the result.

    Args:
        expression: A mathematical expression to evaluate (e.g., "2 + 2", "10 * 5")

    Returns:
        The result of the calculation as a string
    """
    try:
        # Safe evaluation of basic math expressions
        # Note: In production, use a proper math expression parser
        result = eval(expression, {"__builtins__": {}}, {})
        return f"The result of {expression} is {result}"
    except Exception as e:
        return f"Error calculating {expression}: {str(e)}"


@function_tool
def get_weather(location: str) -> str:
    """Get the current weather for a location.

    Args:
        location: The city or location to get weather for

    Returns:
        Weather information for the location
    """
    # Simulated weather data
    weather_data = {
        "san francisco": "Sunny, 68°F",
        "new york": "Cloudy, 45°F",
        "london": "Rainy, 52°F",
        "tokyo": "Clear, 61°F",
    }

    location_lower = location.lower()
    if location_lower in weather_data:
        return f"The weather in {location} is {weather_data[location_lower]}"
    else:
        return f"Weather data not available for {location}. Available cities: {', '.join(weather_data.keys())}"


tools = [calculate, get_weather]

# Create the OpenAI agent
agent = Agent(
    name="BasicAssistant",
    instructions="""You are a helpful assistant that can use tools and skills to solve problems.""",
    tools=tools,
)


materialize_from_bootstrap("/config")
config = KAgentConfig.from_file()

# Create KAgent app
app = KAgentApp(
    agent=agent,
    agent_card=json.loads(Path("/config/agent-card.json").read_text()),
    config=config,
)


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    app.build().run()
