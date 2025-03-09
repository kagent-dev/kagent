import json
import logging
import os
from pathlib import Path

import pytest
from autogen_agentchat.base import Team

logger = logging.getLogger(__name__)


def test_load_agents():
    # Required this be set, but it's unused
    os.environ["OPENAI_API_KEY"] = "fake"
    # load all .json files in the ../agents directory
    # Use the current file's directory as the base path
    base_path = Path(__file__).parent.parent / "agents"
    files = list(base_path.glob("*.json"))
    assert len(files) > 0, "No agents found"
    for file in files:
        with open(file, "r") as f:
            agent_config = json.load(f)
            Team.load_component(agent_config)
