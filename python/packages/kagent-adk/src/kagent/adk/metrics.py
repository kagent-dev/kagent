"""Prometheus metrics for KAgent workflow agents."""

import logging
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from prometheus_client import Gauge

logger = logging.getLogger(__name__)

# Try to import prometheus_client, but make it optional
try:
    from prometheus_client import Gauge

    PROMETHEUS_AVAILABLE = True
except ImportError:
    logger.warning(
        "prometheus_client not installed. Metrics will not be collected. Install with: pip install prometheus-client"
    )
    PROMETHEUS_AVAILABLE = False
    Gauge = None  # type: ignore

# Prometheus metrics for ParallelAgent concurrency tracking
# These are module-level singletons to ensure only one registry per metric

if PROMETHEUS_AVAILABLE:
    kagent_parallel_queue_depth = Gauge(
        "kagent_parallel_queue_depth",
        "Number of sub-agents waiting for semaphore slot",
        labelnames=["agent_name", "namespace"],
    )

    kagent_parallel_active_executions = Gauge(
        "kagent_parallel_active_executions",
        "Number of currently running sub-agents",
        labelnames=["agent_name", "namespace"],
    )

    kagent_parallel_max_workers = Gauge(
        "kagent_parallel_max_workers",
        "Configured maximum concurrent sub-agents",
        labelnames=["agent_name", "namespace"],
    )
else:
    # Create dummy metrics that do nothing
    class DummyGauge:
        """Dummy gauge that does nothing when prometheus_client is not available."""

        def labels(self, **kwargs):
            """Return self to allow chaining."""
            return self

        def set(self, value):
            """No-op set method."""
            pass

        def inc(self, amount=1):
            """No-op increment method."""
            pass

        def dec(self, amount=1):
            """No-op decrement method."""
            pass

    kagent_parallel_queue_depth = DummyGauge()
    kagent_parallel_active_executions = DummyGauge()
    kagent_parallel_max_workers = DummyGauge()


class ParallelAgentMetrics:
    """Context manager for tracking ParallelAgent execution metrics.

    Automatically updates Prometheus metrics for queue depth and active executions
    during sub-agent execution.

    Usage:
        async with ParallelAgentMetrics(agent_name="my-agent", namespace="default"):
            # Execute sub-agent
            await sub_agent.run_async(context)
    """

    def __init__(self, agent_name: str, namespace: str = "default"):
        """Initialize metrics context manager.

        Args:
            agent_name: Name of the parallel agent
            namespace: Kubernetes namespace (default: "default")
        """
        self.agent_name = agent_name
        self.namespace = namespace
        self.active_metric = kagent_parallel_active_executions.labels(agent_name=agent_name, namespace=namespace)
        self.queue_metric = kagent_parallel_queue_depth.labels(agent_name=agent_name, namespace=namespace)

    async def __aenter__(self):
        """Increment active executions when entering context."""
        self.active_metric.inc()
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        """Decrement active executions when exiting context."""
        self.active_metric.dec()
        return False  # Don't suppress exceptions

    def inc_queue_depth(self):
        """Increment queue depth (sub-agent waiting for semaphore)."""
        self.queue_metric.inc()

    def dec_queue_depth(self):
        """Decrement queue depth (sub-agent acquired semaphore)."""
        self.queue_metric.dec()


def set_max_workers_metric(agent_name: str, namespace: str, max_workers: int):
    """Set the max_workers metric for a parallel agent.

    Args:
        agent_name: Name of the parallel agent
        namespace: Kubernetes namespace
        max_workers: Configured maximum concurrent sub-agents
    """
    kagent_parallel_max_workers.labels(agent_name=agent_name, namespace=namespace).set(max_workers)


def reset_metrics(agent_name: str, namespace: str):
    """Reset all metrics for a parallel agent to zero.

    Useful for cleanup when an agent is deleted or restarted.

    Args:
        agent_name: Name of the parallel agent
        namespace: Kubernetes namespace
    """
    kagent_parallel_queue_depth.labels(agent_name=agent_name, namespace=namespace).set(0)
    kagent_parallel_active_executions.labels(agent_name=agent_name, namespace=namespace).set(0)
