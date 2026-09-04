import { describe, expect, it } from "vitest";
import {
  fragmentIssues,
  fragmentsToData,
  newFragmentRow,
  rowsFromData,
} from "./fragmentRows";

/** Rows as the editor holds them, without the ids each test would have to invent. */
const rows = (...pairs: [string, string][]) =>
  pairs.map(([key, value]) => ({ ...newFragmentRow(), key, value }));

describe("rowsFromData", () => {
  it("seeds one row per fragment, ordered by key", () => {
    // Insertion order is deliberately not key order here: `data` is a map on the
    // wire, so the editor cannot inherit an order from it and has to impose one.
    const seeded = rowsFromData({ tone: "Be concise.", escalation: "Hand off." });
    expect(seeded.map((row) => [row.key, row.value])).toEqual([
      ["escalation", "Hand off."],
      ["tone", "Be concise."],
    ]);
  });

  it("gives distinct ids, so editing one row does not rename another", () => {
    const seeded = rowsFromData({ a: "1", b: "2" });
    expect(new Set(seeded.map((row) => row.id)).size).toBe(2);
  });

  it("offers a blank row for a library with no fragments", () => {
    // The API can hand one back — a ConfigMap with no data — and an editor with no
    // rows has nowhere to type and no obvious way to get a row.
    expect(rowsFromData({})).toHaveLength(1);
    expect(rowsFromData({})[0]).toMatchObject({ key: "", value: "" });
  });

  it("round-trips through the payload unchanged", () => {
    const data = { tone: "Be concise.", safety: "Confirm first." };
    expect(fragmentsToData(rowsFromData(data))).toEqual(data);
  });
});

describe("fragmentIssues", () => {
  it("passes a library with at least one distinct key", () => {
    expect(fragmentIssues(rows(["tone", "Be concise."]))).toEqual([]);
  });

  it("rejects rows that would submit an empty map", () => {
    // What the controller rejects too — "at least one template key is required" —
    // so catching it here is the same rule stated before the request.
    expect(fragmentIssues(rows())).toEqual(["Add at least one fragment key."]);
    expect(fragmentIssues(rows(["", "orphaned text"]))).toEqual([
      "Add at least one fragment key.",
    ]);
  });

  it("names a duplicated key, which the payload would silently swallow", () => {
    expect(fragmentIssues(rows(["tone", "first"], ["tone", "second"]))).toEqual([
      'Two fragments share the key "tone".',
    ]);
  });

  it("ignores whitespace-only differences between keys, as the payload does", () => {
    expect(fragmentIssues(rows(["tone", "first"], [" tone ", "second"]))).toEqual([
      'Two fragments share the key "tone".',
    ]);
  });
});
