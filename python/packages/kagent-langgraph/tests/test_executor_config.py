"""Tests for LangGraphAgentExecutorConfig."""

import importlib
import os
from unittest.mock import patch


def test_recursion_limit_default():
    """Test that default recursion_limit is 25 (LangGraph's default)."""
    from kagent.langgraph._executor import LangGraphAgentExecutorConfig

    config = LangGraphAgentExecutorConfig()
    assert config.recursion_limit == 25


def test_recursion_limit_from_env_var():
    """Test that LANGGRAPH_RECURSION_LIMIT env var is picked up."""
    with patch.dict(os.environ, {"LANGGRAPH_RECURSION_LIMIT": "50"}):
        # Re-import to pick up new env var value
        import kagent.langgraph._executor as executor_mod

        importlib.reload(executor_mod)
        config = executor_mod.LangGraphAgentExecutorConfig()
        assert config.recursion_limit == 50

    # Restore default
    os.environ.pop("LANGGRAPH_RECURSION_LIMIT", None)
    importlib.reload(executor_mod)


def test_recursion_limit_explicit_override():
    """Test that explicit config value overrides env var default."""
    from kagent.langgraph._executor import LangGraphAgentExecutorConfig

    config = LangGraphAgentExecutorConfig(recursion_limit=100)
    assert config.recursion_limit == 100
