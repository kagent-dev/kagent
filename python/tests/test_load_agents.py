import json
import logging
import os
from pathlib import Path

import pytest
from autogen_agentchat.base import Team

logger = logging.getLogger(__name__)


# Set up the OpenAI API key as a fixture
@pytest.fixture(scope="module")
def setup_env():
    # Required this be set, but it's unused
    os.environ["OPENAI_API_KEY"] = "fake"


# Get all agent files
def get_agent_files():
    base_path = Path(__file__).parent.parent / "agents"
    files = list(base_path.glob("*.json"))
    assert len(files) > 0, "No agents found"
    return files


# Create a fixture for each agent file
@pytest.fixture(params=get_agent_files())
def agent_file(request):
    return request.param


# Test that loads each agent file individually
def test_load_agent(setup_env, agent_file):
    with open(agent_file, "r") as f:
        agent_config = json.load(f)
        Team.load_component(agent_config)
    logger.info(f"Successfully loaded agent from {agent_file.name}")


# Alternatively, create named fixtures for each agent file
# This allows targeting specific agents in tests if needed
agent_files = get_agent_files()
for file in agent_files:
    fixture_name = f"agent_{file.stem}"
    globals()[fixture_name] = pytest.fixture(lambda file=file: file)
