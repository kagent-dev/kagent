"""Load immutable configuration during golden initialization or from durable storage."""

import hashlib
import json
import os
import random
import stat
import struct
import tempfile
import time
from pathlib import Path
from urllib.parse import urlsplit

import grpc
from kagent.runtime.v1 import bootstrap_pb2, bootstrap_pb2_grpc

from ._grpc import _target_from_url

FORMAT_VERSION = 1
MAX_PAYLOAD_BYTES = 256 << 10
MAX_BUNDLE_BYTES = MAX_PAYLOAD_BYTES + (1 << 10)  # Protobuf metadata allowance.
BOOTSTRAP_DIRECTORY = Path("/data/kagent/bootstrap")


def _digest(bundle) -> str:
    digest = hashlib.sha256(struct.pack(">I", bundle.format_version))
    for field in (bundle.revision.encode("utf-8"), bundle.config_json, bundle.agent_card_json):
        digest.update(struct.pack(">Q", len(field)))
        digest.update(field)
    return digest.hexdigest()


def _is_digest(value) -> bool:
    return isinstance(value, str) and len(value) == 64 and all(c in "0123456789abcdef" for c in value)


def _verify(bundle, descriptor) -> None:
    if bundle.format_version != FORMAT_VERSION or bundle.revision != descriptor["revision"]:
        raise ValueError("bootstrap bundle version or revision mismatch")
    if len(bundle.config_json) + len(bundle.agent_card_json) > MAX_PAYLOAD_BYTES:
        raise ValueError("bootstrap configuration and Agent Card exceed size limit")
    json.loads(bundle.config_json)
    json.loads(bundle.agent_card_json)
    if bundle.sha256 != descriptor["sha256"] or _digest(bundle) != bundle.sha256:
        raise ValueError("bootstrap bundle digest mismatch")


def load_bundle(raw_descriptor: str, directory: Path = BOOTSTRAP_DIRECTORY):
    """Use the pinned local bundle, or fetch and atomically save it before readiness."""
    descriptor = json.loads(raw_descriptor)
    if (
        descriptor.get("format_version") != FORMAT_VERSION
        or not _is_digest(descriptor.get("revision"))
        or not _is_digest(descriptor.get("sha256"))
    ):
        raise ValueError("invalid bootstrap descriptor")
    url = descriptor.get("control_plane_url", "")
    if urlsplit(url).username is not None:
        raise ValueError("bootstrap control plane URL must not contain userinfo")
    target, secure = _target_from_url(url)
    path = directory / "bundle.pb"
    if path.is_symlink() or directory.is_symlink():
        raise ValueError("invalid bootstrap storage path")
    try:
        if not stat.S_ISREG(path.stat().st_mode):
            raise ValueError("invalid durable bootstrap file")
        with path.open("rb") as file:
            raw = file.read(MAX_BUNDLE_BYTES + 1)
        if len(raw) > MAX_BUNDLE_BYTES:
            raise ValueError("durable bootstrap bundle exceeds size limit")
        bundle = bootstrap_pb2.BootstrapBundle.FromString(raw)
        _verify(bundle, descriptor)
        return bundle
    except FileNotFoundError:
        pass

    options = (("grpc.max_receive_message_length", MAX_BUNDLE_BYTES),)
    if secure:
        ca_file = os.getenv("SSL_CERT_FILE")
        roots = Path(ca_file).read_bytes() if ca_file else None
        channel = grpc.secure_channel(target, grpc.ssl_channel_credentials(root_certificates=roots), options)
    else:
        channel = grpc.insecure_channel(target, options)
    # Leave headroom within the 30-second wakeup probe. Exhaustion fails startup;
    # the controller golden retry recreates the template if binding stays unavailable.
    deadline = time.monotonic() + 20
    with channel:
        stub = bootstrap_pb2_grpc.BootstrapServiceStub(channel)
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("bootstrap initialization deadline exceeded")
            try:
                bundle = stub.GetBootstrapConfig(
                    bootstrap_pb2.BootstrapServiceGetBootstrapConfigRequest(),
                    timeout=min(3, remaining),
                    # The gateway replaces this header with the actor JWT.
                    metadata=(("authorization", "Bearer placeholder"),),
                ).bundle
                break
            except grpc.RpcError as error:
                if error.code() not in (grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.DEADLINE_EXCEEDED):
                    raise
                time.sleep(min(random.uniform(0.1, 0.3), max(0, deadline - time.monotonic())))
    _verify(bundle, descriptor)
    directory.mkdir(parents=True, mode=0o700, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=directory, prefix=".bundle-", delete=False) as file:
            temporary = Path(file.name)
            file.write(bundle.SerializeToString())
            file.flush()
            os.fsync(file.fileno())
        temporary.replace(path)
        temporary = None
        parent = os.open(directory, os.O_RDONLY)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    return bundle
