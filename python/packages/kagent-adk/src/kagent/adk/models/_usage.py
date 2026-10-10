def cached_token_count(tokens: int | None) -> int:
    """Normalize cached tokens to the nonnegative int32 range used by Go genai."""
    return min(max(tokens or 0, 0), (1 << 31) - 1)
