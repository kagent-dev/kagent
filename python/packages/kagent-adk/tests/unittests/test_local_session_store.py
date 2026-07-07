"""Tests for durable-dir session storage: KAGENT_SESSION_DB_URL selects the local
DatabaseSessionService and enables the /local/sessions/{id}/events read-through route."""

import asyncio

from a2a.types import AgentCapabilities, AgentCard
from fastapi.testclient import TestClient
from google.adk.events.event import Event
from google.adk.sessions import DatabaseSessionService

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


def seed_local_store(db_url: str, session_id: str, user_id: str, authors: list[str]) -> list[str]:
    """Create a session and append one event per author; returns the event ids."""

    async def _seed() -> list[str]:
        svc = DatabaseSessionService(db_url=db_url)
        session = await svc.create_session(app_name=APP_NAME, user_id=user_id, session_id=session_id)
        ids = []
        for author in authors:
            event = Event(author=author, invocation_id="inv1")
            await svc.append_event(session, event)
            ids.append(event.id)
        return ids

    return asyncio.run(_seed())


def test_local_events_endpoint_serves_controller_wire_shape(tmp_path, monkeypatch):
    # google-adk's DatabaseSessionService uses SQLAlchemy's async engine: the URL must name an
    # async driver (sqlite+aiosqlite), matching what the controller injects for durable-dir agents.
    db_url = f"sqlite+aiosqlite:///{tmp_path}/sessions.db"
    monkeypatch.setenv("KAGENT_SESSION_DB_URL", db_url)
    app = make_kagent_app().build()

    event_ids = seed_local_store(db_url, "s1", "u1", ["user", "model"])

    client = TestClient(app)
    resp = client.get("/local/sessions/s1/events", params={"user_id": "u1"})
    assert resp.status_code == 200
    rows = resp.json()

    # Rows use the controller event wire shape and come back in append order; the data blob
    # round-trips as an ADK Event exactly like rows written through the HTTP session service.
    assert [r["id"] for r in rows] == event_ids
    assert [Event.model_validate_json(r["data"]).author for r in rows] == ["user", "model"]
    assert all(r["created_at"] for r in rows)


def test_local_events_endpoint_empty_for_unknown_session(tmp_path, monkeypatch):
    db_url = f"sqlite+aiosqlite:///{tmp_path}/sessions.db"
    monkeypatch.setenv("KAGENT_SESSION_DB_URL", db_url)
    app = make_kagent_app().build()

    client = TestClient(app)
    resp = client.get("/local/sessions/never-chatted/events", params={"user_id": "u1"})
    # The feature is on, there are just no rows yet: an empty list, NOT a 404 — the controller
    # treats 404 as "runtime does not support durable-dir sessions" and fails loud.
    assert resp.status_code == 200
    assert resp.json() == []


def test_local_events_route_absent_without_env(monkeypatch):
    monkeypatch.delenv("KAGENT_SESSION_DB_URL", raising=False)
    app = make_kagent_app().build()

    client = TestClient(app)
    assert client.get("/local/sessions/s1/events", params={"user_id": "u1"}).status_code == 404
