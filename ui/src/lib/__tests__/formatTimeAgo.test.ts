import { describe, expect, it } from "@jest/globals";
import { formatTimeAgoShort, getLastActivityTimeIso } from "../formatTimeAgo";
import type { Agent } from "@/types";

describe("formatTimeAgoShort", () => {
  it("returns short hours for recent times", () => {
    const now = Date.parse("2026-01-15T12:00:00.000Z");
    const t = "2026-01-15T10:00:00.000Z";
    expect(formatTimeAgoShort(t, now)).toBe("2h ago");
  });

  it("returns em dash for missing or invalid", () => {
    expect(formatTimeAgoShort(undefined, Date.now())).toBe("—");
    expect(formatTimeAgoShort("", Date.now())).toBe("—");
    expect(formatTimeAgoShort("not a date", Date.now())).toBe("—");
  });
});

describe("getLastActivityTimeIso", () => {
  it("picks the latest lastTransitionTime from conditions", () => {
    const agent = {
      metadata: { name: "a" },
      status: {
        conditions: [
          { type: "A", status: "True", lastTransitionTime: "2026-01-01T00:00:00.000Z" },
          { type: "B", status: "True", lastTransitionTime: "2026-06-01T12:00:00.000Z" },
        ],
      },
    } as unknown as Agent;
    expect(getLastActivityTimeIso(agent)).toBe("2026-06-01T12:00:00.000Z");
  });

  it("falls back to creationTimestamp", () => {
    const agent = {
      metadata: { name: "a", creationTimestamp: "2025-12-01T00:00:00.000Z" },
    } as Agent;
    expect(getLastActivityTimeIso(agent)).toBe("2025-12-01T00:00:00.000Z");
  });
});
