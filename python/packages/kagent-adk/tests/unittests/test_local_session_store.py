"""Tests for durable-dir session storage: AgentConfig.session_db_url selects the local
DatabaseSessionService instead of the HTTP KAgentSessionService."""

from a2a.types import AgentCapabilities, AgentCard

import kagent.adk._a2a as _a2a
from kagent.adk import KAgentApp
from kagent.adk.types import AgentConfig, Gemini

APP_NAME = "test-app"


def make_kagent_app(agent_config: AgentConfig | None = None) -> KAgentApp:
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
        agent_config=agent_config,
    )


def config_with_session_db_url(url: str | None) -> AgentConfig:
    return AgentConfig(
        model=Gemini(type="gemini", model="gemini-2.5-flash"),
        description="d",
        instruction="i",
        session_db_url=url,
    )


def test_config_session_db_url_selects_local_store(monkeypatch):
    constructed = {}

    class FakeDatabaseSessionService:
        def __init__(self, db_url):
            constructed["db_url"] = db_url

    monkeypatch.setattr(_a2a, "DatabaseSessionService", FakeDatabaseSessionService)
    make_kagent_app(config_with_session_db_url("sqlite+aiosqlite:////data/sessions.db")).build()

    assert constructed == {"db_url": "sqlite+aiosqlite:////data/sessions.db"}


def test_no_url_selects_kagent_session_service(monkeypatch):
    def boom(*args, **kwargs):
        raise AssertionError("DatabaseSessionService must not be constructed without a session DB URL")

    monkeypatch.setattr(_a2a, "DatabaseSessionService", boom)
    make_kagent_app(config_with_session_db_url(None)).build()
    make_kagent_app(None).build()
