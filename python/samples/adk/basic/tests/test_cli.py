"""Regression test for the basic ADK sample runtime command."""

from pathlib import Path

from kagent.adk import cli


def test_run_loads_basic_sample_and_passes_app_to_uvicorn(monkeypatch):
    monkeypatch.setenv("KAGENT_API_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_GATEWAY_URL", "http://localhost:8083")
    monkeypatch.setenv("KAGENT_NAME", "basic")
    monkeypatch.setenv("KAGENT_NAMESPACE", "default")
    monkeypatch.chdir(Path(__file__).parent.parent)
    monkeypatch.syspath_prepend(str(Path.cwd()))
    captured = {}
    monkeypatch.setattr(cli.uvicorn, "run", lambda app, **kwargs: captured.update(app=app, kwargs=kwargs))

    cli.run("basic", working_dir=".", host="0.0.0.0", local=True)

    assert captured["kwargs"] == {"host": "0.0.0.0", "port": 8080, "workers": 1, "log_level": "info"}
