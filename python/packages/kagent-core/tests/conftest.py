import pytest

from kagent.core.tracing._context_attributes import _allowed_context_mappings


@pytest.fixture(autouse=True)
def clear_allowed_context_mappings_cache():
    """The allowlist is cached for process lifetime; tests change the env var."""
    _allowed_context_mappings.cache_clear()
    yield
    _allowed_context_mappings.cache_clear()
