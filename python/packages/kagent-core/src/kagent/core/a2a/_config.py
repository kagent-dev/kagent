"""HTTP request limits for the A2A adapters."""

DEFAULT_A2A_MAX_CONTENT_LENGTH = 10 * 1024 * 1024


def get_a2a_max_content_length(value: int | None = None) -> int | None:
    """None selects the standalone default; zero explicitly means unlimited."""
    if value is None:
        return DEFAULT_A2A_MAX_CONTENT_LENGTH
    if isinstance(value, bool) or value < 0:
        raise ValueError("max_content_length must be a nonnegative integer")
    return value or None
