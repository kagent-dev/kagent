import { describe, expect, it } from "vitest";
import { stableJson } from "./stableJson";

describe("stableJson", () => {
  it("prints the same text whatever order the keys arrived in", () => {
    const one = { questions: [], header: "Approach", multiSelect: false };
    const other = { multiSelect: false, questions: [], header: "Approach" };

    expect(stableJson(one)).toBe(stableJson(other));
  });

  it("sorts keys at every depth", () => {
    const payload = { b: 1, a: { d: 2, c: 3 } };

    expect(stableJson(payload)).toBe('{\n  "a": {\n    "c": 3,\n    "d": 2\n  },\n  "b": 1\n}');
  });

  it("leaves array order alone, because there the order is the data", () => {
    const payload = { questions: [{ header: "second" }, { header: "first" }] };

    expect(stableJson(payload)).toContain('"second"');
    expect(stableJson(payload).indexOf('"second"')).toBeLessThan(
      stableJson(payload).indexOf('"first"'),
    );
  });

  it("passes through values that have no keys to sort", () => {
    expect(stableJson("text")).toBe('"text"');
    expect(stableJson(null)).toBe("null");
    expect(stableJson(7)).toBe("7");
  });

  it("keeps a null inside an object rather than treating it as a nested object", () => {
    expect(stableJson({ b: null, a: 1 })).toBe('{\n  "a": 1,\n  "b": null\n}');
  });
});
