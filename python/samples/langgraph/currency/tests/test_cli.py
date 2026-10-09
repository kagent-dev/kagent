"""Regression tests for the currency console entry point."""

import importlib

from fastapi.testclient import TestClient


def test_main_passes_healthy_app_to_uvicorn(monkeypatch, tmp_path):
    monkeypatch.setenv("OPENAI_API_KEY", "fake")
    monkeypatch.setenv("KAGENT_API_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_GATEWAY_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_NAME", "currency")
    monkeypatch.setenv("KAGENT_NAMESPACE", "default")
    checkpoint_db = tmp_path / "currency-checkpoints.sqlite"
    monkeypatch.setenv("KAGENT_CHECKPOINT_DB", str(checkpoint_db))

    cli = importlib.import_module("currency.cli")
    captured = {}

    def run(app, **kwargs):
        captured["app"] = app
        captured["kwargs"] = kwargs

    monkeypatch.setattr(cli.uvicorn, "run", run)
    cli.main()

    response = TestClient(captured["app"]).get("/health")

    assert response.status_code == 200
    assert response.text == "OK"
    assert checkpoint_db.exists()
    assert captured["kwargs"] == {"host": "0.0.0.0", "port": 8080, "log_level": "info"}
