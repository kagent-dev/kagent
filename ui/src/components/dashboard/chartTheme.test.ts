import { describe, expect, it } from "vitest";
import type { TooltipItem } from "chart.js";
import { horizontalBarOptions } from "./chartTheme";

/**
 * The tooltip reads out a count, so it has to agree with it.
 *
 * One string for the unit made it say "1 tools reported" on every server that
 * reported exactly one — the commonest non-zero case there is.
 */
function tooltipFor(value: number): string {
  const options = horizontalBarOptions({
    one: "tool reported",
    many: "tools reported",
  });
  const label = options.plugins?.tooltip?.callbacks?.label;
  if (typeof label !== "function") throw new Error("no label callback");

  // Only `parsed.x` is read; the rest of a real item is irrelevant here.
  const item = { parsed: { x: value } } as unknown as TooltipItem<"bar">;
  return String(label.call({} as never, item));
}

describe("horizontalBarOptions — the tooltip's unit", () => {
  it("uses the singular for exactly one", () => {
    expect(tooltipFor(1)).toBe("1 tool reported");
  });

  it("uses the plural for more than one", () => {
    expect(tooltipFor(3)).toBe("3 tools reported");
  });

  it("uses the plural for none, which is what English does", () => {
    expect(tooltipFor(0)).toBe("0 tools reported");
  });
});
