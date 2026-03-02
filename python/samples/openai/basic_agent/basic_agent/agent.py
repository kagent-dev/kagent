"""Basic OpenAI Agent with KAgent integration.

This sample demonstrates how to create a simple OpenAI agent that can:
- Answer questions
- Use tools (calculate, get weather)
- Use skills from the skills directory
- Maintain conversation history via sessions
"""

import logging
from pathlib import Path

from a2a.types import AgentCard
from agents.agent import Agent
from agents.tool import function_tool
from kagent.core import KAgentConfig
from kagent.openai import KAgentApp

logger = logging.getLogger(__name__)

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


# Agent card for A2A protocol
agent_card = AgentCard(
    name="basic-openai-agent",
    description="A basic OpenAI agent with calculator and weather tools",
    url="localhost:8000",
    version="0.1.0",
    capabilities={"streaming": True},
    defaultInputModes=["text"],
    defaultOutputModes=["text"],
    skills=[
        {
            "id": "basic",
            "name": "Basic Assistant",
            "description": "Can perform calculations and get weather information",
            "tags": ["calculator", "weather", "assistant"],
        }
    ],
)

config = KAgentConfig()

# Create KAgent app
app = KAgentApp(
    agent=agent,
    agent_card=agent_card,
    config=config,
)


# Build the FastAPI application
fastapi_app = app.build()


if __name__ == "__main__":
    import uvicorn

    logging.basicConfig(level=logging.INFO)
    logger.info("Starting Basic OpenAI Agent...")
    logger.info("Server will be available at http://0.0.0.0:8080")

    uvicorn.run(fastapi_app, host="0.0.0.0", port=8080)
