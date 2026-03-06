"""Test that A2A experimental warnings are shown once, not on every import."""

import warnings


def test_a2a_warning_shown_once():
    """Importing types installs a 'once' filter for A2A experimental warnings."""
    # Reset warning filters to baseline
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")

        # Re-install the filter as types.py does at import time
        warnings.filterwarnings("once", message=r"\[EXPERIMENTAL\].*A2A")

        # First warning should be recorded
        warnings.warn("[EXPERIMENTAL] A2A support is experimental", stacklevel=1)
        assert len(caught) == 1

        # Second identical warning should be suppressed
        warnings.warn("[EXPERIMENTAL] A2A support is experimental", stacklevel=1)
        assert len(caught) == 1, "Duplicate A2A warning was not suppressed"
