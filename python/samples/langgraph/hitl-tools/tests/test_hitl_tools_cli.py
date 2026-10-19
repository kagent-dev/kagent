"""Regression tests for the HITL tools console entry point."""

import importlib

from fastapi.testclient import TestClient


def test_main_passes_healthy_app_to_uvicorn(monkeypatch, configure_runtime):
    monkeypatch.setenv("OPENAI_API_KEY", "fake")
    configure_runtime("hitl-tools")

    cli = importlib.import_module("hitl_tools.cli")
    captured = {}
    monkeypatch.setattr(cli.uvicorn, "run", lambda app, **kwargs: captured.update(app=app, kwargs=kwargs))

    cli.main()

    assert TestClient(captured["app"]).get("/health").text == "OK"
    assert captured["kwargs"] == {"host": "0.0.0.0", "port": 8080, "log_level": "info"}
