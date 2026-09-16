"""Regression tests for the poem flow console entry point."""

import importlib

from fastapi.testclient import TestClient


def test_main_passes_healthy_app_to_uvicorn(monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "fake")
    monkeypatch.setenv("KAGENT_API_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_GATEWAY_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_NAME", "poem-flow")
    monkeypatch.setenv("KAGENT_NAMESPACE", "default")
    module = importlib.import_module("poem_flow.main")
    captured = {}
    monkeypatch.setattr(module.uvicorn, "run", lambda app, **kwargs: captured.update(app=app, kwargs=kwargs))

    module.main()

    assert TestClient(captured["app"]).get("/health").text == "OK"
    assert captured["kwargs"] == {"host": "0.0.0.0", "port": 8080, "log_level": "info"}
