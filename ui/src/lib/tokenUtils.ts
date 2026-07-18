/** Token count/formatting helpers for the chat UI. */

/** Formats a token count compactly: 950 -> "950", 12345 -> "12.3k", 2500000 -> "2.5M". */
export function formatTokens(count: number): string {
  if (!Number.isFinite(count) || count < 0) return "0";
  if (count >= 1_000_000) return `${trimDecimal(count / 1_000_000)}M`;
  if (count >= 1_000) {
    const formatted = trimDecimal(count / 1_000);
    // Rounding can roll 999950..999999 up to "1000"; show it as the next unit.
    return formatted === "1000" ? "1M" : `${formatted}k`;
  }
  return String(Math.round(count));
}

/** One decimal place, dropping a trailing ".0". */
const trimDecimal = (value: number): string => {
  const fixed = value.toFixed(1);
  return fixed.endsWith(".0") ? fixed.slice(0, -2) : fixed;
};

/**
 * Best-effort context window sizes (tokens) by model-name prefix. Longest
 * prefix wins. This is a UI-side approximation; a ModelConfig.contextWindow
 * CRD field is the planned authoritative source (see reports/ui-polish-plan.md).
 */
const MODEL_CONTEXT_WINDOWS: Array<[prefix: string, tokens: number]> = [
  // OpenAI
  ["gpt-4.1", 1_000_000],
  ["gpt-4o", 128_000],
  ["gpt-4-turbo", 128_000],
  ["gpt-4", 8_192],
  ["gpt-3.5", 16_385],
  ["o1", 200_000],
  ["o3", 200_000],
  ["o4", 200_000],
  // Anthropic
  ["claude", 200_000],
  // Google
  ["gemini-1.5", 1_000_000],
  ["gemini-2", 1_000_000],
  ["gemini", 1_000_000],
  // Meta / open weights (common serving defaults)
  ["llama-3.1", 128_000],
  ["llama3.1", 128_000],
  ["llama", 8_192],
  ["mistral-large", 128_000],
  ["mixtral", 32_768],
  ["qwen", 32_768],
  ["deepseek", 64_000],
];

/** Returns the context window for a model name, or undefined when unknown. */
export function getModelContextWindow(model: string | undefined): number | undefined {
  if (!model) return undefined;
  const normalized = model.toLowerCase();
  let best: number | undefined;
  let bestLength = 0;
  for (const [prefix, tokens] of MODEL_CONTEXT_WINDOWS) {
    if (normalized.startsWith(prefix) && prefix.length > bestLength) {
      best = tokens;
      bestLength = prefix.length;
    }
  }
  return best;
}
