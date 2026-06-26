import json
from types import SimpleNamespace

import pytest

from kagent.adk._reflect_retry_plugin import KAgentReflectAndRetryToolPlugin
from kagent.adk.types import AgentConfig, ReliabilityConfig


def _make_agent_config(**extra) -> AgentConfig:
    config = {
        "model": {"type": "openai", "model": "gpt-4"},
        "description": "test agent",
        "instruction": "test instruction",
    }
    config.update(extra)
    return AgentConfig.model_validate_json(json.dumps(config))


class TestReliabilityConfigParsing:
    def test_defaults_unset(self):
        config = _make_agent_config()
        assert config.reliability is None

    def test_tool_retries(self):
        config = _make_agent_config(reliability={"tool_retries": 3})
        assert config.reliability == ReliabilityConfig(tool_retries=3)

    def test_max_llm_calls(self):
        config = _make_agent_config(reliability={"tool_retries": 3, "max_llm_calls": 25})
        assert config.reliability == ReliabilityConfig(tool_retries=3, max_llm_calls=25)

    def test_debug_logging(self):
        config = _make_agent_config(reliability={"debug_logging": True})
        assert config.reliability is not None
        assert config.reliability.debug_logging is True


class TestKAgentReflectAndRetryToolPlugin:
    @pytest.mark.asyncio
    async def test_mcp_error_result_detected(self):
        plugin = KAgentReflectAndRetryToolPlugin(max_retries=2, throw_exception_if_retry_exceeded=False)
        result = {"content": [{"type": "text", "text": "apply failed: exit status 1"}], "isError": True}
        error = await plugin.extract_error_from_result(tool=None, tool_args={}, tool_context=None, result=result)
        assert error == result

    @pytest.mark.asyncio
    async def test_successful_result_not_detected(self):
        plugin = KAgentReflectAndRetryToolPlugin(max_retries=2, throw_exception_if_retry_exceeded=False)
        result = {"content": [{"type": "text", "text": "applied"}], "isError": False}
        assert await plugin.extract_error_from_result(tool=None, tool_args={}, tool_context=None, result=result) is None
        assert (
            await plugin.extract_error_from_result(tool=None, tool_args={}, tool_context=None, result="plain") is None
        )

    @pytest.mark.asyncio
    async def test_exception_path_still_handled(self):
        """Tools that raise exceptions go through the inherited on_tool_error_callback."""
        plugin = KAgentReflectAndRetryToolPlugin(max_retries=2, throw_exception_if_retry_exceeded=False)
        tool = SimpleNamespace(name="kubectl_apply")
        ctx = SimpleNamespace(invocation_id="inv-1")
        response = await plugin.on_tool_error_callback(
            tool=tool, tool_args={"manifest": "bad"}, tool_context=ctx, error=RuntimeError("boom")
        )
        assert response is not None
        assert "kubectl_apply" in str(response)

    @pytest.mark.asyncio
    async def test_exception_and_iserror_share_failure_counter(self):
        """An exception followed by an isError result counts as 2 attempts for the same tool."""
        plugin = KAgentReflectAndRetryToolPlugin(max_retries=2, throw_exception_if_retry_exceeded=False)
        tool = SimpleNamespace(name="kubectl_apply")
        ctx = SimpleNamespace(invocation_id="inv-1")
        error_result = {"content": [{"type": "text", "text": "failed"}], "isError": True}

        # Attempt 1: exception
        await plugin.on_tool_error_callback(tool=tool, tool_args={}, tool_context=ctx, error=RuntimeError("boom"))
        # Attempt 2: isError result (routed via after_tool_callback)
        await plugin.after_tool_callback(tool=tool, tool_args={}, tool_context=ctx, result=error_result)
        # Attempt 3: exceeds max_retries=2 -> retry-exceeded guidance instead of raising
        response = await plugin.after_tool_callback(tool=tool, tool_args={}, tool_context=ctx, result=error_result)
        assert response is not None
        assert "2" in str(response)  # mentions the retry limit

    @pytest.mark.asyncio
    async def test_success_resets_counter(self):
        plugin = KAgentReflectAndRetryToolPlugin(max_retries=1, throw_exception_if_retry_exceeded=False)
        tool = SimpleNamespace(name="kubectl_apply")
        ctx = SimpleNamespace(invocation_id="inv-1")
        error_result = {"isError": True}
        ok_result = {"content": [{"type": "text", "text": "applied"}], "isError": False}

        await plugin.after_tool_callback(tool=tool, tool_args={}, tool_context=ctx, result=error_result)
        # Success resets the per-tool counter
        assert await plugin.after_tool_callback(tool=tool, tool_args={}, tool_context=ctx, result=ok_result) is None
        # Next failure is attempt 1 again -> reflection guidance, not retry-exceeded
        response = await plugin.after_tool_callback(tool=tool, tool_args={}, tool_context=ctx, result=error_result)
        assert response is not None
