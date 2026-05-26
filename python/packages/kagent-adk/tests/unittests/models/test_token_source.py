"""Unit tests for GDCHTokenSource.

These tests verify that the kagent ModelConfig TLS configuration is the
authoritative source of truth for the GDCH STS call: the ``ca_cert_path``
baked into the SA JSON by ``gdcloud`` must be normalized at refresh time,
not honored as-is.
"""

import datetime
import json
from unittest import mock

import pytest

from kagent.adk.models._token_source import GDCHTokenSource

SAMPLE_SA_JSON = {
    "type": "gdch_service_account",
    "format_version": "1",
    "project": "test-project",
    "name": "projects/test-project/serviceAccounts/test-sa",
    "private_key_id": "abc123",
    "private_key": "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n",
    "token_uri": "https://gdch.example/sts/v1/token",
    "ca_cert_path": "/home/operator/laptop-only/ca.crt",
}

AUDIENCE = "https://gdch.example/inference"


@pytest.fixture
def sa_file(tmp_path):
    """Write a sample GDCH SA JSON to disk and return its path."""
    p = tmp_path / "sa.json"
    p.write_text(json.dumps(SAMPLE_SA_JSON))
    return p


def _make_fake_creds(token: str = "test-token", expiry: datetime.datetime | None = None):
    """Build a fake google-auth credentials object with the methods we touch."""
    fake = mock.MagicMock()
    fake.with_gdch_audience.return_value = fake
    fake.token = token
    fake.expiry = expiry
    return fake


def test_exchange_overwrites_ca_cert_path_when_kagent_ca_set(sa_file):
    """When kagent ModelConfig provides a CA path, the JSON's ca_cert_path is rewritten."""
    mounted_ca = "/etc/ssl/certs/custom/ca.crt"
    src = GDCHTokenSource(
        service_account_path=str(sa_file),
        audience=AUDIENCE,
        ca_cert_path=mounted_ca,
    )

    with mock.patch("google.oauth2.gdch_credentials.ServiceAccountCredentials") as mock_cls:
        mock_cls.from_service_account_info.return_value = _make_fake_creds()
        src._exchange()

    info_passed = mock_cls.from_service_account_info.call_args.args[0]
    assert info_passed["ca_cert_path"] == mounted_ca


def test_exchange_strips_ca_cert_path_when_no_kagent_ca(sa_file):
    """When kagent provides no CA, the JSON's ca_cert_path is removed entirely."""
    src = GDCHTokenSource(
        service_account_path=str(sa_file),
        audience=AUDIENCE,
        ca_cert_path=None,
    )

    with mock.patch("google.oauth2.gdch_credentials.ServiceAccountCredentials") as mock_cls:
        mock_cls.from_service_account_info.return_value = _make_fake_creds()
        src._exchange()

    info_passed = mock_cls.from_service_account_info.call_args.args[0]
    assert "ca_cert_path" not in info_passed


def test_exchange_strips_ca_cert_path_when_disable_verify(sa_file):
    """tls_disable_verify takes priority: ca_cert_path is stripped and session.verify is False."""
    src = GDCHTokenSource(
        service_account_path=str(sa_file),
        audience=AUDIENCE,
        ca_cert_path="/etc/ssl/certs/custom/ca.crt",
        tls_disable_verify=True,
    )

    fake_creds = _make_fake_creds()
    with (
        mock.patch("google.oauth2.gdch_credentials.ServiceAccountCredentials") as mock_cls,
        mock.patch("requests.Session") as mock_session_cls,
    ):
        mock_cls.from_service_account_info.return_value = fake_creds
        session = mock_session_cls.return_value
        # Session is used as a context manager; have __enter__ yield the same mock
        # so attributes set inside the `with` block are observable here.
        session.__enter__.return_value = session
        src._exchange()

    info_passed = mock_cls.from_service_account_info.call_args.args[0]
    assert "ca_cert_path" not in info_passed
    assert session.verify is False


def test_exchange_does_not_modify_sa_json_file(sa_file):
    """Normalization happens in-memory; the on-disk SA JSON is untouched."""
    original = sa_file.read_bytes()
    src = GDCHTokenSource(
        service_account_path=str(sa_file),
        audience=AUDIENCE,
        ca_cert_path="/etc/ssl/certs/custom/ca.crt",
    )

    with mock.patch("google.oauth2.gdch_credentials.ServiceAccountCredentials") as mock_cls:
        mock_cls.from_service_account_info.return_value = _make_fake_creds()
        src._exchange()

    assert sa_file.read_bytes() == original


def test_exchange_uses_credential_expiry(sa_file):
    """When creds expose an expiry, the token cache honors it."""
    src = GDCHTokenSource(
        service_account_path=str(sa_file),
        audience=AUDIENCE,
    )

    future = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(minutes=10)
    with mock.patch("google.oauth2.gdch_credentials.ServiceAccountCredentials") as mock_cls:
        mock_cls.from_service_account_info.return_value = _make_fake_creds(expiry=future)
        token = src._exchange()

    assert token == "test-token"
    # _expiry is set relative to time.monotonic(); just confirm it advanced past 0.
    assert src._expiry > 0


async def test_get_token_caches_within_expiry_buffer(sa_file):
    """Subsequent get_token calls within the 30 s buffer return the cached token."""
    src = GDCHTokenSource(service_account_path=str(sa_file), audience=AUDIENCE)

    with mock.patch.object(src, "_exchange", return_value="cached-token") as mock_exchange:
        first = await src.get_token()
        second = await src.get_token()

    assert first == "cached-token"
    assert second == "cached-token"
    assert mock_exchange.call_count == 1
