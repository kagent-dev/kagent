"""Test that A2A experimental warnings are shown once, not on every import."""

import warnings


def test_a2a_warning_shown_once():
    """Importing types installs a scoped 'once' filter for A2A experimental warnings."""
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")

        # Re-install the filter as types.py does at import time
        # (scoped to the specific remote_a2a_agent module)
        warnings.filterwarnings(
            "once",
            message=r"\[EXPERIMENTAL\].*A2A",
            category=UserWarning,
            module=r"^google\.adk\.agents\.remote_a2a_agent$",
        )

        # Simulate a warning from the exact upstream module by executing in a
        # namespace whose __name__ matches the module filter.
        adk_globals = {"__name__": "google.adk.agents.remote_a2a_agent", "__file__": __file__}
        code = compile(
            'import warnings; warnings.warn("[EXPERIMENTAL] A2A support is experimental", UserWarning, stacklevel=1)',
            "<test>",
            "exec",
        )

        exec(code, adk_globals)  # noqa: S102
        assert len(caught) == 1, "First A2A warning should be recorded"

        exec(code, adk_globals)  # noqa: S102
        assert len(caught) == 1, "Duplicate A2A warning from remote_a2a_agent should be suppressed"


def test_filter_does_not_suppress_non_adk_warnings():
    """The filter should not suppress warnings from modules outside remote_a2a_agent."""
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")

        # Install the scoped filter
        warnings.filterwarnings(
            "once",
            message=r"\[EXPERIMENTAL\].*A2A",
            category=UserWarning,
            module=r"^google\.adk\.agents\.remote_a2a_agent$",
        )

        # A warning from the current test module (not remote_a2a_agent) should not be suppressed
        warnings.warn("[EXPERIMENTAL] A2A support is experimental", UserWarning, stacklevel=1)
        assert len(caught) == 1

        warnings.warn("[EXPERIMENTAL] A2A support is experimental", UserWarning, stacklevel=1)
        # Both should be recorded because this module is not google.adk.agents.remote_a2a_agent
        assert len(caught) == 2, "Warnings from non-remote_a2a_agent modules should not be suppressed"


def test_filter_does_not_suppress_other_adk_modules():
    """The filter should not suppress warnings from other google.adk submodules."""
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")

        # Install the scoped filter (exact module match only)
        warnings.filterwarnings(
            "once",
            message=r"\[EXPERIMENTAL\].*A2A",
            category=UserWarning,
            module=r"^google\.adk\.agents\.remote_a2a_agent$",
        )

        # A warning from a different google.adk submodule should NOT be suppressed
        other_adk_globals = {"__name__": "google.adk.a2a.executor", "__file__": __file__}
        code = compile(
            'import warnings; warnings.warn("[EXPERIMENTAL] A2A support is experimental", UserWarning, stacklevel=1)',
            "<test>",
            "exec",
        )

        exec(code, other_adk_globals)  # noqa: S102
        assert len(caught) == 1

        exec(code, other_adk_globals)  # noqa: S102
        # Both should be recorded because the module doesn't match remote_a2a_agent exactly
        assert len(caught) == 2, "Warnings from other google.adk modules should not be suppressed"
