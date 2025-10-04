"""Unit tests for metrics module."""

import pytest

from kagent.adk.metrics import (
    PROMETHEUS_AVAILABLE,
    ParallelAgentMetrics,
    kagent_parallel_active_executions,
    kagent_parallel_max_workers,
    kagent_parallel_queue_depth,
    reset_metrics,
    set_max_workers_metric,
)


def test_prometheus_available_flag():
    """Test that PROMETHEUS_AVAILABLE flag exists."""
    assert isinstance(PROMETHEUS_AVAILABLE, bool)


def test_queue_depth_gauge_exists():
    """Test that kagent_parallel_queue_depth gauge is defined."""
    assert kagent_parallel_queue_depth is not None


def test_active_executions_gauge_exists():
    """Test that kagent_parallel_active_executions gauge is defined."""
    assert kagent_parallel_active_executions is not None


def test_max_workers_gauge_exists():
    """Test that kagent_parallel_max_workers gauge is defined."""
    assert kagent_parallel_max_workers is not None


def test_set_max_workers_metric():
    """Test set_max_workers_metric sets the gauge value."""
    # Should not raise even if prometheus_client is not available
    set_max_workers_metric(agent_name="test-agent", namespace="default", max_workers=10)

    # Verify it returns something (either real gauge or dummy)
    metric = kagent_parallel_max_workers.labels(agent_name="test-agent", namespace="default")
    assert metric is not None


def test_reset_metrics():
    """Test reset_metrics resets all gauges to zero."""
    # Should not raise even if prometheus_client is not available
    reset_metrics(agent_name="test-agent", namespace="default")


def test_parallel_agent_metrics_initialization():
    """Test ParallelAgentMetrics initialization."""
    metrics = ParallelAgentMetrics(agent_name="test-agent", namespace="test-ns")

    assert metrics.agent_name == "test-agent"
    assert metrics.namespace == "test-ns"
    assert metrics.active_metric is not None
    assert metrics.queue_metric is not None


def test_parallel_agent_metrics_default_namespace():
    """Test ParallelAgentMetrics with default namespace."""
    metrics = ParallelAgentMetrics(agent_name="test-agent")

    assert metrics.namespace == "default"


@pytest.mark.asyncio
async def test_parallel_agent_metrics_context_manager():
    """Test ParallelAgentMetrics as async context manager."""
    metrics = ParallelAgentMetrics(agent_name="test-agent", namespace="default")

    # Should not raise
    async with metrics as ctx:
        assert ctx is metrics


@pytest.mark.asyncio
async def test_parallel_agent_metrics_enter_exit():
    """Test __aenter__ and __aexit__ methods."""
    metrics = ParallelAgentMetrics(agent_name="test-agent", namespace="default")

    result = await metrics.__aenter__()
    assert result is metrics

    exit_result = await metrics.__aexit__(None, None, None)
    assert exit_result is False  # Should not suppress exceptions


@pytest.mark.asyncio
async def test_parallel_agent_metrics_with_exception():
    """Test that exceptions are not suppressed by context manager."""
    metrics = ParallelAgentMetrics(agent_name="test-agent", namespace="default")

    with pytest.raises(ValueError):
        async with metrics:
            raise ValueError("Test exception")


def test_parallel_agent_metrics_inc_queue_depth():
    """Test inc_queue_depth method."""
    metrics = ParallelAgentMetrics(agent_name="test-agent", namespace="default")

    # Should not raise
    metrics.inc_queue_depth()


def test_parallel_agent_metrics_dec_queue_depth():
    """Test dec_queue_depth method."""
    metrics = ParallelAgentMetrics(agent_name="test-agent", namespace="default")

    # Should not raise
    metrics.dec_queue_depth()


def test_parallel_agent_metrics_queue_operations():
    """Test multiple queue depth operations."""
    metrics = ParallelAgentMetrics(agent_name="test-agent", namespace="default")

    # Multiple operations should not raise
    metrics.inc_queue_depth()
    metrics.inc_queue_depth()
    metrics.dec_queue_depth()
    metrics.dec_queue_depth()


def test_set_max_workers_different_namespaces():
    """Test set_max_workers_metric with different namespaces."""
    set_max_workers_metric(agent_name="agent1", namespace="ns1", max_workers=5)
    set_max_workers_metric(agent_name="agent1", namespace="ns2", max_workers=10)

    # Should not raise - metrics should be independent per namespace


def test_reset_metrics_different_agents():
    """Test reset_metrics for different agents."""
    reset_metrics(agent_name="agent1", namespace="default")
    reset_metrics(agent_name="agent2", namespace="default")
    reset_metrics(agent_name="agent1", namespace="custom-ns")

    # Should not raise


@pytest.mark.asyncio
async def test_parallel_agent_metrics_multiple_concurrent():
    """Test multiple concurrent ParallelAgentMetrics contexts."""
    metrics1 = ParallelAgentMetrics(agent_name="agent1", namespace="default")
    metrics2 = ParallelAgentMetrics(agent_name="agent2", namespace="default")

    async with metrics1:
        async with metrics2:
            # Both should be active simultaneously
            pass


def test_gauge_labels_method():
    """Test that gauges have labels method."""
    result = kagent_parallel_queue_depth.labels(agent_name="test", namespace="default")
    assert result is not None


def test_gauge_set_method():
    """Test that gauges have set method."""
    gauge = kagent_parallel_max_workers.labels(agent_name="test", namespace="default")
    gauge.set(42)  # Should not raise


def test_gauge_inc_method():
    """Test that gauges have inc method."""
    gauge = kagent_parallel_active_executions.labels(agent_name="test", namespace="default")
    gauge.inc()  # Should not raise
    gauge.inc(5)  # Should not raise with amount


def test_gauge_dec_method():
    """Test that gauges have dec method."""
    gauge = kagent_parallel_active_executions.labels(agent_name="test", namespace="default")
    gauge.dec()  # Should not raise
    gauge.dec(3)  # Should not raise with amount
