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
