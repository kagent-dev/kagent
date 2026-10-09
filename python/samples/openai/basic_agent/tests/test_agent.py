"""Regression tests for the basic OpenAI agent console entry point."""

import importlib

from fastapi.testclient import TestClient


def test_main_passes_healthy_app_to_uvicorn(monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "fake")
    monkeypatch.setenv("KAGENT_API_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_GATEWAY_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_NAME", "basic-openai-agent")
    monkeypatch.setenv("KAGENT_NAMESPACE", "default")

    agent = importlib.import_module("basic_agent.agent")
    captured = {}

    def run(app, **kwargs):
        captured["app"] = app
        captured["kwargs"] = kwargs

    monkeypatch.setattr("uvicorn.run", run)
    agent.main()

    response = TestClient(captured["app"]).get("/health")

    assert response.status_code == 200
    assert response.text == "OK"
    assert captured["kwargs"] == {"host": "0.0.0.0", "port": 8080}
