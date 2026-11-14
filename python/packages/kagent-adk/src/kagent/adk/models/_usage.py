def cached_token_count(tokens: int | None) -> int:
    """Normalize cached tokens to the nonnegative int32 range used by Go genai."""
    return min(max(tokens or 0, 0), (1 << 31) - 1)


def bedrock_prompt_token_count(usage: dict[str, int]) -> int:
    """Fold Bedrock's disjoint cache read/write counters into the total prompt."""
    prompt = (
        (usage.get("inputTokens") or 0)
        + max(usage.get("cacheReadInputTokens") or 0, 0)
        + max(usage.get("cacheWriteInputTokens") or 0, 0)
    )
    return cached_token_count(prompt)
