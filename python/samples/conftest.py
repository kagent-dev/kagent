"""Shared fixtures for Python sample tests."""

from collections.abc import Callable

import pytest
from kagent.core import KAgentConfig


@pytest.fixture
def configure_runtime(monkeypatch: pytest.MonkeyPatch) -> Callable[[str], None]:
    """Make runtime-file loading return a valid standalone test configuration."""

    def configure(name: str) -> None:
        def load(cls, path="/config/config.json"):
            del path
            return cls(
                api_url="http://localhost:8083",
                gateway_url="http://localhost:8083",
                name=name,
                namespace="default",
            )

        monkeypatch.setattr(KAgentConfig, "from_file", classmethod(load))

    return configure
