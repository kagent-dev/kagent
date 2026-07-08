"""Tests for durable-dir session storage: KAGENT_SESSION_DB_URL selects the local
DatabaseSessionService instead of the HTTP KAgentSessionService."""

from a2a.types import AgentCapabilities, AgentCard

import kagent.adk._a2a as _a2a
from kagent.adk import KAgentApp

APP_NAME = "test-app"


def make_kagent_app() -> KAgentApp:
    card = AgentCard(
        name=APP_NAME,
        description="test agent",
        url="http://localhost:8080",
        version="0.0.1",
        capabilities=AgentCapabilities(),
        default_input_modes=["text"],
        default_output_modes=["text"],
        skills=[],
    )
    # root_agent_factory is only invoked per-request by the executor, never during build.
    return KAgentApp(
        root_agent_factory=lambda: None,
        agent_card=card,
        kagent_url="http://kagent-controller:8083",
        app_name=APP_NAME,
    )


def test_env_selects_local_database_session_service(monkeypatch):
    db_url = "sqlite+aiosqlite:////data/sessions.db"
    monkeypatch.setenv("KAGENT_SESSION_DB_URL", db_url)

    constructed = {}

    class FakeDatabaseSessionService:
        def __init__(self, db_url):
            constructed["db_url"] = db_url

    monkeypatch.setattr(_a2a, "DatabaseSessionService", FakeDatabaseSessionService)
    make_kagent_app().build()

    assert constructed == {"db_url": db_url}


def test_no_env_selects_kagent_session_service(monkeypatch):
    monkeypatch.delenv("KAGENT_SESSION_DB_URL", raising=False)

    def boom(*args, **kwargs):
        raise AssertionError("DatabaseSessionService must not be constructed without KAGENT_SESSION_DB_URL")

    monkeypatch.setattr(_a2a, "DatabaseSessionService", boom)
    make_kagent_app().build()
