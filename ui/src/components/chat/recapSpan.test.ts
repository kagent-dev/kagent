import { describe, expect, it } from "vitest";
import { recapSpan } from "./recapSpan";

describe("recapSpan", () => {
  const span = (minutes: number) => recapSpan("2026-08-21T10:00:00Z", new Date(Date.UTC(2026, 7, 21, 10, 0) + minutes * 60_000).toISOString());

  it.each([
    [0, "the last minute"],
    [1, "the last minute"],
    [6, "the last 6 minutes"],
    [59, "the last 59 minutes"],
    [60, "the last hour"],
    [150, "the last 3 hours"],
    [24 * 60, "the last day"],
    [3 * 24 * 60, "the last 3 days"],
  ])("reads %i minutes as %s", (minutes, expected) => {
    expect(span(minutes)).toBe(expected);
  });

  it("is empty when a time is missing, unreadable or out of order", () => {
    expect(recapSpan(undefined, "2026-08-21T10:30:00Z")).toBe("");
    expect(recapSpan("", "2026-08-21T10:30:00Z")).toBe("");
    expect(recapSpan("not a time", "2026-08-21T10:30:00Z")).toBe("");
    expect(recapSpan("2026-08-21T11:00:00Z", "2026-08-21T10:30:00Z")).toBe("");
  });
});
