import asyncio

import pytest
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset

from kagent.adk._agent_executor import A2aAgentExecutor
from kagent.adk._mcp_toolset import KAgentMcpToolset


class _RunnerCloseRaises:
    async def close(self):
        raise RuntimeError("boom")


class _RunnerCloseCancelScopeError:
    async def close(self):
        raise RuntimeError("Attempted to exit cancel scope in a different task than it was entered in")


class _RunnerCloseSleeps:
    async def close(self):
        await asyncio.sleep(10)


@pytest.mark.asyncio
async def test_safe_close_runner_reraises_unexpected_runner_close_error():
    executor = A2aAgentExecutor(runner=lambda: None)
    with pytest.raises(RuntimeError, match="boom"):
        await executor._safe_close_runner(_RunnerCloseRaises())


@pytest.mark.asyncio
async def test_safe_close_runner_suppresses_known_anyio_cancel_scope_error():
    executor = A2aAgentExecutor(runner=lambda: None)
    await executor._safe_close_runner(_RunnerCloseCancelScopeError())


@pytest.mark.asyncio
async def test_safe_close_runner_propagates_caller_cancellation():
    executor = A2aAgentExecutor(runner=lambda: None)
    task = asyncio.create_task(executor._safe_close_runner(_RunnerCloseSleeps()))
    await asyncio.sleep(0)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task


@pytest.mark.asyncio
async def test_mcp_toolset_close_suppresses_known_anyio_cancel_scope_error(monkeypatch):
    async def _close_raise_cancel_scope_error(self):
        raise RuntimeError("Attempted to exit cancel scope in a different task than it was entered in")

    monkeypatch.setattr(McpToolset, "close", _close_raise_cancel_scope_error)
    toolset = object.__new__(KAgentMcpToolset)
    await toolset.close()


@pytest.mark.asyncio
async def test_mcp_toolset_close_reraises_unexpected_error(monkeypatch):
    async def _close_raise_unexpected_error(self):
        raise ValueError("unexpected failure")

    monkeypatch.setattr(McpToolset, "close", _close_raise_unexpected_error)
    toolset = object.__new__(KAgentMcpToolset)
    with pytest.raises(ValueError, match="unexpected failure"):
        await toolset.close()


@pytest.mark.asyncio
async def test_mcp_toolset_close_reraises_non_cross_task_cancel_scope_error(monkeypatch):
    async def _close_raise_non_cross_task_cancel_scope_error(self):
        raise RuntimeError("cancel scope timeout while shutting down")

    monkeypatch.setattr(McpToolset, "close", _close_raise_non_cross_task_cancel_scope_error)
    toolset = object.__new__(KAgentMcpToolset)
    with pytest.raises(RuntimeError, match="cancel scope timeout while shutting down"):
        await toolset.close()


@pytest.mark.asyncio
async def test_mcp_toolset_close_propagates_cancelled_error(monkeypatch):
    async def _close_raise_cancelled(self):
        raise asyncio.CancelledError("external cancellation")

    monkeypatch.setattr(McpToolset, "close", _close_raise_cancelled)
    toolset = object.__new__(KAgentMcpToolset)
    with pytest.raises(asyncio.CancelledError, match="external cancellation"):
        await toolset.close()
