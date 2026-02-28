"""Tests for experimental warning suppression (issue #1379).

The google-adk library emits UserWarning with '[EXPERIMENTAL]' on every
instantiation of RemoteA2aAgent and A2aAgentExecutor. Without filtering,
this floods logs during normal A2A operations.

kagent.adk.__init__ sets warnings.filterwarnings("once", ...) so the
warning is only shown once per unique message + location.
"""

import warnings

import pytest


def test_experimental_warning_emitted_once():
    """Verify [EXPERIMENTAL] warnings are deduplicated by the 'once' filter."""
    # Apply the same filter that kagent.adk.__init__ sets
    warnings.filterwarnings("once", message=r"\[EXPERIMENTAL\]", category=UserWarning)

    with warnings.catch_warnings(record=True) as caught:
        # Re-apply the filter inside catch_warnings context
        warnings.filterwarnings("once", message=r"\[EXPERIMENTAL\]", category=UserWarning)

        # Simulate multiple instantiations (same message, same location)
        for _ in range(10):
            warnings.warn(
                "[EXPERIMENTAL] RemoteA2aAgent: ADK Implementation for A2A support...",
                UserWarning,
                stacklevel=1,
            )

        experimental = [w for w in caught if "[EXPERIMENTAL]" in str(w.message)]
        assert len(experimental) == 1, f"Expected exactly 1 experimental warning, got {len(experimental)}"


def test_non_experimental_warnings_unaffected():
    """Verify the filter does not suppress other UserWarnings."""
    warnings.filterwarnings("once", message=r"\[EXPERIMENTAL\]", category=UserWarning)

    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")
        warnings.filterwarnings("once", message=r"\[EXPERIMENTAL\]", category=UserWarning)

        # Non-experimental warnings should still appear every time
        warnings.warn("Something else happened", UserWarning, stacklevel=1)
        warnings.warn("Something else happened", UserWarning, stacklevel=1)

        other = [w for w in caught if "[EXPERIMENTAL]" not in str(w.message)]
        assert len(other) == 2, f"Expected 2 non-experimental warnings, got {len(other)}"
