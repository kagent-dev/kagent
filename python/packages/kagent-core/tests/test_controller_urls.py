import pytest

from kagent.core._grpc import _target_from_url


@pytest.mark.parametrize(
    ("url", "target", "secure"),
    [
        ("http://localhost:8083", "localhost:8083", False),
        ("https://api.example.com", "api.example.com", True),
    ],
)
def test_target_from_url(url: str, target: str, secure: bool) -> None:
    assert _target_from_url(url) == (target, secure)


def test_target_from_url_rejects_paths() -> None:
    with pytest.raises(ValueError, match="scheme and authority"):
        _target_from_url("https://api.example.com/path")
