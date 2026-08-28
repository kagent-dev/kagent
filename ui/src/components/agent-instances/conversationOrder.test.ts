import { describe, expect, it } from "vitest";
import type { AgentInstance } from "@/api";
import { byNewestFirst } from "./conversationOrder";

/** Only the fields the comparator reads; the rest of an instance is irrelevant here. */
const at = (id: string, createdAt: string) => ({ id, createdAt }) as AgentInstance;

describe("byNewestFirst", () => {
  it("puts the most recently started conversation at the top", () => {
    const rows = [
      at("older", "2026-08-11T16:40:00Z"),
      at("newest", "2026-08-21T07:55:00Z"),
      at("middle", "2026-08-18T09:12:00Z"),
    ];

    expect([...rows].sort(byNewestFirst).map((row) => row.id)).toEqual([
      "newest",
      "middle",
      "older",
    ]);
  });

  it("gives equal timestamps one fixed order rather than whatever the sort did", () => {
    // A rail that reshuffled equal rows between reads is the defect this removes, so
    // the tie-break is part of the behaviour and not an implementation detail.
    const same = "2026-08-18T09:12:00Z";
    const one = [at("b", same), at("a", same), at("c", same)];
    const other = [at("c", same), at("b", same), at("a", same)];

    expect([...one].sort(byNewestFirst).map((r) => r.id)).toEqual(["a", "b", "c"]);
    expect([...other].sort(byNewestFirst).map((r) => r.id)).toEqual(["a", "b", "c"]);
  });

  it("does not throw a conversation with no timestamp out of the list", () => {
    // `createdAt` is empty on at least one fixture, and a comparator that treated that
    // as unorderable would drop the row or move it unpredictably. It sorts last, which
    // is where a conversation with no known start belongs.
    const rows = [at("undated", ""), at("dated", "2026-08-18T09:12:00Z")];

    expect([...rows].sort(byNewestFirst).map((r) => r.id)).toEqual(["dated", "undated"]);
  });
});
