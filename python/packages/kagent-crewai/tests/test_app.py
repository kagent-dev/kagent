"""Tests for CrewAI application transport ownership."""

from contextlib import asynccontextmanager
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock

from kagent.crewai import KAgentApp


def _agent_card() -> dict:
    return {
        "name": "test-agent",
        "description": "Test agent",
        "url": "http://localhost:8080",
        "version": "0.1.0",
        "capabilities": {"streaming": True},
        "defaultInputModes": ["text"],
        "defaultOutputModes": ["text"],
        "skills": [],
    }


async def test_build_reuses_injected_controller_client_and_closes_it(monkeypatch):
    client = MagicMock()
    client.close = AsyncMock()

    @asynccontextmanager
    async def lifespan(_):
        try:
            yield
        finally:
            await client.close()

    client.lifespan.return_value = lifespan
    task_store_factory = MagicMock(return_value=MagicMock())
    client_factory = MagicMock()
    monkeypatch.setattr("kagent.crewai._a2a.KAgentTaskStore", task_store_factory)
    monkeypatch.setattr("kagent.crewai._a2a.AsyncControllerClient", client_factory)

    config = SimpleNamespace(
        app_name="test__NS__agent",
        grpc_url="localhost:8084",
        name="agent",
        namespace="test",
    )
    app = KAgentApp(
        crew=MagicMock(),
        agent_card=_agent_card(),
        config=config,
        controller_client=client,
        tracing=False,
    ).build()

    client_factory.assert_not_called()
    task_store_factory.assert_called_once_with(client)
    async with app.router.lifespan_context(app):
        client.close.assert_not_awaited()
    client.close.assert_awaited_once_with()
