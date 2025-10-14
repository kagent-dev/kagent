"""Memory profiling utilities for testing memory management in workflows."""

import gc
import sys
import tracemalloc
from typing import Any, Dict

import psutil


class MemoryProfiler:
    """Helper class for tracking memory usage in tests."""

    def __init__(self):
        """Initialize memory profiler."""
        self.process = psutil.Process()
        self.baseline_rss = 0
        self.baseline_tracemalloc = 0
        self.peak_rss = 0
        self.snapshots = []

    def start_profiling(self) -> None:
        """Start memory profiling and capture baseline."""
        # Force garbage collection to get clean baseline
        gc.collect()
        gc.collect()
        gc.collect()

        # Start tracemalloc for detailed Python memory tracking
        tracemalloc.start()

        # Capture baseline RSS (Resident Set Size)
        self.baseline_rss = self.process.memory_info().rss
        self.baseline_tracemalloc = tracemalloc.get_traced_memory()[0]
        self.peak_rss = self.baseline_rss

    def capture_snapshot(self, label: str = "snapshot") -> Dict[str, Any]:
        """Capture current memory state.

        Args:
            label: Label for this snapshot

        Returns:
            Dictionary with memory metrics
        """
        gc.collect()  # Force GC before measurement

        rss = self.process.memory_info().rss
        current, peak = tracemalloc.get_traced_memory()

        snapshot = {
            "label": label,
            "rss_bytes": rss,
            "rss_mb": rss / (1024 * 1024),
            "rss_delta_mb": (rss - self.baseline_rss) / (1024 * 1024),
            "tracemalloc_bytes": current,
            "tracemalloc_mb": current / (1024 * 1024),
            "tracemalloc_delta_mb": (current - self.baseline_tracemalloc) / (1024 * 1024),
            "peak_tracemalloc_mb": peak / (1024 * 1024),
        }

        self.snapshots.append(snapshot)

        if rss > self.peak_rss:
            self.peak_rss = rss

        return snapshot

    def stop_profiling(self) -> Dict[str, Any]:
        """Stop profiling and return final metrics.

        Returns:
            Dictionary with final memory metrics
        """
        final_snapshot = self.capture_snapshot("final")

        tracemalloc.stop()

        return {
            "baseline_rss_mb": self.baseline_rss / (1024 * 1024),
            "final_rss_mb": final_snapshot["rss_mb"],
            "peak_rss_mb": self.peak_rss / (1024 * 1024),
            "delta_rss_mb": final_snapshot["rss_delta_mb"],
            "delta_tracemalloc_mb": final_snapshot["tracemalloc_delta_mb"],
            "snapshots": self.snapshots,
        }

    def assert_memory_released(self, threshold_percent: float = 10.0) -> None:
        """Assert that memory returned to baseline within threshold.

        Args:
            threshold_percent: Acceptable memory increase percentage from baseline

        Raises:
            AssertionError: If memory not released within threshold
        """
        gc.collect()
        gc.collect()
        gc.collect()

        final_rss = self.process.memory_info().rss
        delta_mb = (final_rss - self.baseline_rss) / (1024 * 1024)
        delta_percent = (delta_mb / (self.baseline_rss / (1024 * 1024))) * 100

        assert (
            delta_percent <= threshold_percent
        ), f"Memory not released: {delta_mb:.2f} MB increase ({delta_percent:.1f}%), threshold is {threshold_percent}%"

    def get_memory_summary(self) -> str:
        """Get human-readable memory summary.

        Returns:
            String with memory usage summary
        """
        if not self.snapshots:
            return "No snapshots captured"

        lines = ["Memory Profiling Summary:", "=" * 50]
        lines.append(f"Baseline RSS: {self.baseline_rss / (1024 * 1024):.2f} MB")
        lines.append(f"Peak RSS: {self.peak_rss / (1024 * 1024):.2f} MB")
        lines.append("")
        lines.append("Snapshots:")

        for snapshot in self.snapshots:
            lines.append(f"  {snapshot['label']:20s}: {snapshot['rss_mb']:8.2f} MB (Δ {snapshot['rss_delta_mb']:+7.2f} MB)")

        return "\n".join(lines)


def get_current_memory_mb() -> float:
    """Get current process memory usage in MB.

    Returns:
        Current RSS memory in megabytes
    """
    process = psutil.Process()
    return process.memory_info().rss / (1024 * 1024)


def force_garbage_collection() -> None:
    """Force aggressive garbage collection."""
    gc.collect()
    gc.collect()
    gc.collect()


def get_object_count(obj_type: type) -> int:
    """Count live objects of a specific type.

    Args:
        obj_type: Type to count

    Returns:
        Number of live objects
    """
    force_garbage_collection()
    return sum(1 for obj in gc.get_objects() if isinstance(obj, obj_type))


def check_for_leaked_references(obj: Any) -> int:
    """Check for lingering references to an object.

    Args:
        obj: Object to check for references

    Returns:
        Number of references (should be 1 if no leaks)
    """
    force_garbage_collection()
    return sys.getrefcount(obj) - 1  # Subtract 1 for the argument reference

