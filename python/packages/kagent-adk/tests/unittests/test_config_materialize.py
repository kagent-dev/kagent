import json
import os

import pytest
from kagent.core import _config_materialize
from kagent.runtime.v1 import bootstrap_pb2


def test_materializes_bootstrap_bundle(tmp_path, monkeypatch):
    bundle = bootstrap_pb2.BootstrapBundle(
        config_json=b'{"header":"__KAGENT_ENV[KAGENT_CREDENTIAL_TEST]__"}',
        agent_card_json=b'{"name":"test"}',
    )
    monkeypatch.setenv("KAGENT_BOOTSTRAP_JSON", "descriptor")
    monkeypatch.setenv("KAGENT_CREDENTIAL_TEST", "Bearer secret")
    monkeypatch.delenv("KAGENT_TOKEN", raising=False)
    monkeypatch.setattr(_config_materialize, "load_bundle", lambda raw: bundle)
    config_dir = tmp_path / "config"
    _config_materialize.materialize_from_bootstrap(str(config_dir))
    assert json.loads((config_dir / "config.json").read_text()) == {"header": "Bearer secret"}
    assert json.loads((config_dir / "agent-card.json").read_text()) == {"name": "test"}
    assert os.stat(config_dir / "config.json").st_mode & 0o777 == 0o600


def test_requires_bootstrap_descriptor(tmp_path, monkeypatch):
    monkeypatch.delenv("KAGENT_BOOTSTRAP_JSON", raising=False)
    monkeypatch.setenv("KAGENT_CONFIG_JSON", "{}")
    with pytest.raises(KeyError, match="KAGENT_BOOTSTRAP_JSON"):
        _config_materialize.materialize_from_bootstrap(str(tmp_path))


def test_named_byo_uses_revision_config_and_card(tmp_path, monkeypatch):
    from unittest.mock import Mock

    from a2a.types import AgentCard
    from kagent.core._runtime_config import RuntimeConfig

    from kagent.adk import cli
    from kagent.adk.types import Config

    config = Config(
        runtime=RuntimeConfig(
            name="revision-agent", namespace="test", control_plane_url="http://controller", gateway_url="http://gateway"
        )
    )
    card = AgentCard(name="revision-agent", version="1")
    load_revision = Mock(return_value=(config, card))
    monkeypatch.setattr(cli, "_load_revision", load_revision)
    server = Mock()
    build = Mock(return_value=server)
    monkeypatch.setattr(cli, "KAgentApp", build)
    monkeypatch.setattr(cli, "bootstrap", Mock())
    (tmp_path / "sample").mkdir()
    (tmp_path / "sample" / "__init__.py").touch()
    monkeypatch.syspath_prepend(str(tmp_path))

    cli.run("sample", working_dir=str(tmp_path))

    load_revision.assert_called_once_with("/config")
    assert build.call_args.args[1] == card
    assert build.call_args.kwargs["config"] == config
    server.build.return_value.run.assert_called_once_with()
