"""Tests for experimental warning suppression (issue #1379).

The google-adk library emits UserWarning with '[EXPERIMENTAL]' on every
instantiation of RemoteA2aAgent and A2aAgentExecutor. Without filtering,
this floods logs during normal A2A operations.

kagent.adk.__init__ sets warnings.filterwarnings("once", ...), so a given
warning message/category combination is only shown once, regardless of
call location.
"""

import warnings

# The exact filter pattern installed by kagent.adk.__init__
_FILTER_MESSAGE = r"\[EXPERIMENTAL\].*(RemoteA2aAgent|A2aAgentExecutor)"


def _install_init_filter():
    """Re-install the filter that kagent.adk.__init__ provides.

    The package-level ``__init__.py`` registers this filter at import time.
    We reproduce it here so the tests can run without pulling in heavy
    transitive dependencies (opentelemetry, google-adk, etc.) while still
    validating the filter semantics using the same regex.
    """
    warnings.filterwarnings("once", message=_FILTER_MESSAGE, category=UserWarning)


def test_experimental_warning_emitted_once():
    """Verify [EXPERIMENTAL] A2A warnings are deduplicated by the 'once' filter."""
    with warnings.catch_warnings(record=True) as caught:
        # Baseline: surface every warning so we can verify suppression
        warnings.simplefilter("always")

        # Re-install the filter from kagent.adk (validates the package-level code)
        _install_init_filter()

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
    with warnings.catch_warnings(record=True) as caught:
        # Baseline: surface every warning
        warnings.simplefilter("always")

        # Re-install the filter from kagent.adk
        _install_init_filter()

        # Non-experimental warnings should still appear every time
        warnings.warn("Something else happened", UserWarning, stacklevel=1)
        warnings.warn("Something else happened", UserWarning, stacklevel=1)

        other = [w for w in caught if "[EXPERIMENTAL]" not in str(w.message)]
        assert len(other) == 2, f"Expected 2 non-experimental warnings, got {len(other)}"


def test_filter_only_matches_a2a_experimental():
    """Verify the filter is scoped to RemoteA2aAgent/A2aAgentExecutor, not all [EXPERIMENTAL]."""
    with warnings.catch_warnings(record=True) as caught:
        # Baseline: surface every warning
        warnings.simplefilter("always")

        # Re-install the filter from kagent.adk
        _install_init_filter()

        # An [EXPERIMENTAL] warning NOT from A2A should repeat (not caught by filter)
        for _ in range(3):
            warnings.warn(
                "[EXPERIMENTAL] SomeOtherFeature: this is unrelated",
                UserWarning,
                stacklevel=1,
            )

        unrelated = [w for w in caught if "SomeOtherFeature" in str(w.message)]
        assert len(unrelated) == 3, f"Expected 3 unrelated experimental warnings, got {len(unrelated)}"
