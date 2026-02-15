"""Tests for LangGraphAgentExecutorConfig."""

import importlib
import os
from unittest.mock import patch

import pytest


def test_recursion_limit_default():
    """Test that default recursion_limit is 25 (LangGraph's default)."""
    from kagent.langgraph._executor import LangGraphAgentExecutorConfig

    config = LangGraphAgentExecutorConfig()
    assert config.recursion_limit == 25


def test_recursion_limit_from_env_var():
    """Test that LANGGRAPH_RECURSION_LIMIT env var is picked up at instance creation."""
    import kagent.langgraph._executor as executor_mod

    with patch.dict(os.environ, {"LANGGRAPH_RECURSION_LIMIT": "50"}):
        config = executor_mod.LangGraphAgentExecutorConfig()
        assert config.recursion_limit == 50


def test_recursion_limit_explicit_override():
    """Test that explicit config value overrides env var default."""
    from kagent.langgraph._executor import LangGraphAgentExecutorConfig

    config = LangGraphAgentExecutorConfig(recursion_limit=100)
    assert config.recursion_limit == 100


def test_recursion_limit_rejects_zero():
    """Test that recursion_limit=0 is rejected by gt=0 validation."""
    from kagent.langgraph._executor import LangGraphAgentExecutorConfig

    with pytest.raises(Exception):
        LangGraphAgentExecutorConfig(recursion_limit=0)


def test_recursion_limit_rejects_negative():
    """Test that negative recursion_limit is rejected by gt=0 validation."""
    from kagent.langgraph._executor import LangGraphAgentExecutorConfig

    with pytest.raises(Exception):
        LangGraphAgentExecutorConfig(recursion_limit=-5)
