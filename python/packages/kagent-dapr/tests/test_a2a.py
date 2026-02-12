from unittest.mock import Mock, patch

import pytest
from dapr_agents import DurableAgent
from fastapi import FastAPI
from kagent.core import KAgentConfig
from kagent.dapr import KAgentApp


def _make_agent_card() -> dict:
    return {
        "name": "test-agent",
        "description": "A test agent",
        "url": "http://localhost:8080",
        "version": "0.1.0",
        "defaultInputModes": ["text"],
        "defaultOutputModes": ["text"],
        "skills": [
            {
                "id": "test-skill",
                "name": "Test Skill",
                "description": "A test skill",
                "tags": [],
            }
        ],
        "capabilities": {},
    }


@pytest.fixture
def durable_agent():
    agent = Mock(spec=DurableAgent)
    agent.start = Mock()
    agent.agent_workflow = Mock()
    return agent


@pytest.fixture
def config():
    return KAgentConfig(url="http://localhost:8083", name="test-app", namespace="test-ns")


@patch("kagent.dapr._durable.wf.DaprWorkflowClient")
def test_build_returns_fastapi_app(mock_wf_client, durable_agent, config):
    """build() returns a FastAPI instance."""
    kagent_app = KAgentApp(agent=durable_agent, agent_card=_make_agent_card(), config=config)
    app = kagent_app.build()
    assert isinstance(app, FastAPI)


@patch("kagent.dapr._durable.wf.DaprWorkflowClient")
def test_build_has_health_endpoint(mock_wf_client, durable_agent, config):
    """The built app has a /health route."""
    kagent_app = KAgentApp(agent=durable_agent, agent_card=_make_agent_card(), config=config)
    app = kagent_app.build()
    route_paths = [r.path for r in app.routes]
    assert "/health" in route_paths


@patch("kagent.dapr._durable.wf.DaprWorkflowClient")
def test_build_has_thread_dump_endpoint(mock_wf_client, durable_agent, config):
    """The built app has a /thread_dump route."""
    kagent_app = KAgentApp(agent=durable_agent, agent_card=_make_agent_card(), config=config)
    app = kagent_app.build()
    route_paths = [r.path for r in app.routes]
    assert "/thread_dump" in route_paths


@patch("kagent.dapr._durable.wf.DaprWorkflowClient")
def test_build_accepts_durable_agent(mock_wf_client, durable_agent, config):
    """KAgentApp accepts DurableAgent."""
    kagent_app = KAgentApp(agent=durable_agent, agent_card=_make_agent_card(), config=config)
    app = kagent_app.build()
    assert isinstance(app, FastAPI)
