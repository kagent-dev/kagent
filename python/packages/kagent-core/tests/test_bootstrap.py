import json
from pathlib import Path
from unittest.mock import MagicMock

import pytest
from google.protobuf.message import DecodeError
from kagent.runtime.v1 import bootstrap_pb2

from kagent.core import _bootstrap


@pytest.fixture
def bundle_and_descriptor():
    vector_path = Path(__file__).resolve().parents[4] / "go/api/bootstrap/testdata/digest.json"
    vector = json.loads(vector_path.read_text())
    bundle = bootstrap_pb2.BootstrapServiceGetBootstrapConfigResponse(
        format_version=vector["format_version"],
        revision=vector["revision"],
        config_json=vector["config_json"].encode(),
        agent_card_json=vector["agent_card_json"].encode(),
        sha256=vector["sha256"],
    )
    descriptor = dict(
        format_version=1, control_plane_url="http://controller:8083", revision=bundle.revision, sha256=bundle.sha256
    )
    return bundle, descriptor


def test_digest_matches_go_vector(bundle_and_descriptor):
    bundle, descriptor = bundle_and_descriptor
    assert _bootstrap._digest(bundle) == bundle.sha256
    _bootstrap._verify(bundle, descriptor)


def test_fetch_then_restore_without_network(tmp_path, monkeypatch, bundle_and_descriptor):
    bundle, descriptor = bundle_and_descriptor
    channel = MagicMock()
    stub = MagicMock()
    stub.GetBootstrapConfig.return_value = bundle
    monkeypatch.setattr(_bootstrap.grpc, "insecure_channel", lambda *args, **kwargs: channel)
    monkeypatch.setattr(_bootstrap.bootstrap_pb2_grpc, "BootstrapServiceStub", lambda _: stub)
    directory = tmp_path / "bootstrap"
    assert _bootstrap.load_bundle(json.dumps(descriptor), directory) == bundle
    assert stub.GetBootstrapConfig.call_count == 1
    assert stub.GetBootstrapConfig.call_args.kwargs["metadata"] == (("authorization", "Bearer placeholder"),)
    assert (directory / "bundle.pb").stat().st_mode & 0o777 == 0o600
    stub.GetBootstrapConfig.side_effect = AssertionError("restore must not fetch")
    assert _bootstrap.load_bundle(json.dumps(descriptor), directory) == bundle
    (directory / "bundle.pb").write_bytes(b"corrupt")
    with pytest.raises(DecodeError):
        _bootstrap.load_bundle(json.dumps(descriptor), directory)
    assert stub.GetBootstrapConfig.call_count == 1


def test_rejects_other_revision(tmp_path, bundle_and_descriptor):
    bundle, descriptor = bundle_and_descriptor
    (tmp_path / "bundle.pb").write_bytes(bundle.SerializeToString())
    descriptor["revision"] = "b" * 64
    with pytest.raises(ValueError, match="revision mismatch"):
        _bootstrap.load_bundle(json.dumps(descriptor), tmp_path)


@pytest.mark.parametrize("instruction_bytes", [48 << 10, 128 << 10, _bootstrap.MAX_PAYLOAD_BYTES - 4096])
def test_large_adk_bundle_survives_local_restore(tmp_path, bundle_and_descriptor, instruction_bytes):
    bundle, descriptor = bundle_and_descriptor
    instruction = "x" * instruction_bytes + "large-adk-instruction-end"
    bundle.config_json = json.dumps({"agent": {"instruction": instruction}}).encode()
    bundle.sha256 = _bootstrap._digest(bundle)
    descriptor["sha256"] = bundle.sha256
    (tmp_path / "bundle.pb").write_bytes(bundle.SerializeToString())

    restored = _bootstrap.load_bundle(json.dumps(descriptor), tmp_path)

    assert json.loads(restored.config_json)["agent"]["instruction"] == instruction
    assert restored.config_json == bundle.config_json


def test_oversized_adk_bundle_is_rejected(tmp_path, bundle_and_descriptor):
    bundle, descriptor = bundle_and_descriptor
    bundle.config_json = json.dumps({"agent": {"instruction": "x" * _bootstrap.MAX_PAYLOAD_BYTES}}).encode()
    bundle.sha256 = _bootstrap._digest(bundle)
    descriptor["sha256"] = bundle.sha256
    (tmp_path / "bundle.pb").write_bytes(bundle.SerializeToString())

    with pytest.raises(ValueError, match="exceed size limit"):
        _bootstrap.load_bundle(json.dumps(descriptor), tmp_path)


def test_combined_payload_limit_is_256_kib(tmp_path, bundle_and_descriptor):
    bundle, descriptor = bundle_and_descriptor
    # Valid JSON totaling exactly 256 KiB, including both configuration and card.
    bundle.config_json = b'"' + b"x" * ((256 << 10) - len(bundle.agent_card_json) - 2) + b'"'
    bundle.sha256 = _bootstrap._digest(bundle)
    descriptor["sha256"] = bundle.sha256
    (tmp_path / "bundle.pb").write_bytes(bundle.SerializeToString())
    assert _bootstrap.load_bundle(json.dumps(descriptor), tmp_path) == bundle

    # A trailing space keeps the JSON valid while exceeding the combined budget by one byte.
    bundle.config_json += b" "
    bundle.sha256 = _bootstrap._digest(bundle)
    descriptor["sha256"] = bundle.sha256
    (tmp_path / "bundle.pb").write_bytes(bundle.SerializeToString())
    with pytest.raises(ValueError, match="exceed size limit"):
        _bootstrap.load_bundle(json.dumps(descriptor), tmp_path)
