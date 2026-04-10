import asyncio
import json
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from pydantic import ValidationError

from kagent.adk._agent_executor import A2aAgentExecutor, A2aAgentExecutorConfig, _compute_retry_delay
from kagent.adk.types import AgentConfig, RetryPolicyConfig


def _make_agent_config_json(**retry_kwargs) -> dict:
    config = {
        "model": {"type": "openai", "model": "gpt-4"},
        "description": "test agent",
        "instruction": "test instruction",
    }
    if retry_kwargs:
        config["retry_policy"] = retry_kwargs
    return config


class TestRetryPolicyConfigParsing:
    def test_no_retry_policy(self):
        config = AgentConfig.model_validate(_make_agent_config_json())
        assert config.retry_policy is None

    def test_retry_policy_defaults(self):
        data = _make_agent_config_json(max_retries=3)
        config = AgentConfig.model_validate(data)
        assert config.retry_policy is not None
        assert config.retry_policy.max_retries == 3
        assert config.retry_policy.initial_retry_delay_seconds == 1.0
        assert config.retry_policy.max_retry_delay_seconds is None

    def test_retry_policy_all_fields(self):
        data = _make_agent_config_json(
            max_retries=5,
            initial_retry_delay_seconds=0.5,
            max_retry_delay_seconds=30.0,
        )
        config = AgentConfig.model_validate(data)
        assert config.retry_policy.max_retries == 5
        assert config.retry_policy.initial_retry_delay_seconds == 0.5
        assert config.retry_policy.max_retry_delay_seconds == 30.0

    def test_retry_policy_json_roundtrip(self):
        data = _make_agent_config_json(
            max_retries=3,
            initial_retry_delay_seconds=2.0,
            max_retry_delay_seconds=60.0,
        )
        config = AgentConfig.model_validate(data)
        dumped = json.loads(config.model_dump_json())
        assert dumped["retry_policy"]["max_retries"] == 3
        assert dumped["retry_policy"]["initial_retry_delay_seconds"] == 2.0
        assert dumped["retry_policy"]["max_retry_delay_seconds"] == 60.0


class TestRetryExecution:
    @pytest.mark.asyncio
    async def test_no_retry_on_success(self):
        """Successful execution should not retry."""
        mock_runner = MagicMock()
        executor = A2aAgentExecutor(
            runner=mock_runner,
            config=A2aAgentExecutorConfig(
                stream=False,
                retry_policy=RetryPolicyConfig(max_retries=3, initial_retry_delay_seconds=0.01),
            ),
        )
        context = MagicMock()
        context.message = MagicMock()
        context.current_task = None
        context.task_id = "test-task"
        context.context_id = "test-context"
        event_queue = AsyncMock()

        with patch.object(executor, "_execute_impl", new_callable=AsyncMock) as mock_impl:
            await executor.execute(context, event_queue)
            assert mock_impl.call_count == 1

    @pytest.mark.asyncio
    async def test_retry_on_failure(self):
        """Failed execution should retry up to max_retries."""
        mock_runner = MagicMock()
        executor = A2aAgentExecutor(
            runner=mock_runner,
            config=A2aAgentExecutorConfig(
                stream=False,
                retry_policy=RetryPolicyConfig(max_retries=2, initial_retry_delay_seconds=0.01),
            ),
        )
        context = MagicMock()
        context.message = MagicMock()
        context.current_task = None
        context.task_id = "test-task"
        context.context_id = "test-context"
        event_queue = AsyncMock()

        with patch.object(
            executor,
            "_execute_impl",
            new_callable=AsyncMock,
            side_effect=RuntimeError("transient error"),
        ) as mock_impl:
            await executor.execute(context, event_queue)
            # 1 initial + 2 retries = 3 total
            assert mock_impl.call_count == 3

    @pytest.mark.asyncio
    async def test_no_retry_on_cancelled_error(self):
        """CancelledError should never be retried."""
        mock_runner = MagicMock()
        executor = A2aAgentExecutor(
            runner=mock_runner,
            config=A2aAgentExecutorConfig(
                stream=False,
                retry_policy=RetryPolicyConfig(max_retries=3, initial_retry_delay_seconds=0.01),
            ),
        )
        context = MagicMock()
        context.message = MagicMock()
        context.current_task = None
        context.task_id = "test-task"
        context.context_id = "test-context"
        event_queue = AsyncMock()

        with patch.object(
            executor,
            "_execute_impl",
            new_callable=AsyncMock,
            side_effect=asyncio.CancelledError("cancelled"),
        ) as mock_impl:
            await executor.execute(context, event_queue)
            assert mock_impl.call_count == 1

    @pytest.mark.asyncio
    async def test_retry_succeeds_on_second_attempt(self):
        """Retry should stop after first success."""
        mock_runner = MagicMock()
        executor = A2aAgentExecutor(
            runner=mock_runner,
            config=A2aAgentExecutorConfig(
                stream=False,
                retry_policy=RetryPolicyConfig(max_retries=3, initial_retry_delay_seconds=0.01),
            ),
        )
        context = MagicMock()
        context.message = MagicMock()
        context.current_task = None
        context.task_id = "test-task"
        context.context_id = "test-context"
        event_queue = AsyncMock()

        call_count = 0

        async def fail_then_succeed(*args, **kwargs):
            nonlocal call_count
            call_count += 1
            if call_count == 1:
                raise RuntimeError("transient error")

        with patch.object(executor, "_execute_impl", new_callable=AsyncMock, side_effect=fail_then_succeed):
            await executor.execute(context, event_queue)
            assert call_count == 2


class TestRetryDelayComputation:
    def test_exponential_backoff(self):
        policy = RetryPolicyConfig(max_retries=5, initial_retry_delay_seconds=1.0)
        assert _compute_retry_delay(0, policy) == 1.0
        assert _compute_retry_delay(1, policy) == 2.0
        assert _compute_retry_delay(2, policy) == 4.0
        assert _compute_retry_delay(3, policy) == 8.0

    def test_exponential_backoff_with_max(self):
        policy = RetryPolicyConfig(
            max_retries=5,
            initial_retry_delay_seconds=1.0,
            max_retry_delay_seconds=5.0,
        )
        assert _compute_retry_delay(0, policy) == 1.0
        assert _compute_retry_delay(1, policy) == 2.0
        assert _compute_retry_delay(2, policy) == 4.0
        assert _compute_retry_delay(3, policy) == 5.0  # capped
        assert _compute_retry_delay(4, policy) == 5.0  # still capped
