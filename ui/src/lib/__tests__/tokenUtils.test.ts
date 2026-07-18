import { formatTokens, getModelContextWindow } from "@/lib/tokenUtils";

describe("formatTokens", () => {
  it.each([
    [0, "0"],
    [999, "999"],
    [1000, "1k"],
    [1500, "1.5k"],
    [12345, "12.3k"],
    [999_949, "999.9k"],
    [999_950, "1M"],
    [1_000_000, "1M"],
    [2_500_000, "2.5M"],
  ])("formats %d as %s", (input, expected) => {
    expect(formatTokens(input)).toBe(expected);
  });

  it("handles invalid values", () => {
    expect(formatTokens(-5)).toBe("0");
    expect(formatTokens(NaN)).toBe("0");
  });
});

describe("getModelContextWindow", () => {
  it("matches known prefixes with longest-prefix priority", () => {
    expect(getModelContextWindow("gpt-4o-mini")).toBe(128_000);
    expect(getModelContextWindow("gpt-4-0613")).toBe(8_192);
    expect(getModelContextWindow("gpt-4.1-nano")).toBe(1_000_000);
    expect(getModelContextWindow("claude-sonnet-4-20250514")).toBe(200_000);
    expect(getModelContextWindow("gemini-1.5-pro")).toBe(1_000_000);
  });

  it("is case-insensitive and returns undefined for unknown models", () => {
    expect(getModelContextWindow("Claude-Opus-4")).toBe(200_000);
    expect(getModelContextWindow("my-custom-model")).toBeUndefined();
    expect(getModelContextWindow(undefined)).toBeUndefined();
  });
});
