import { describe, expect, it } from "vitest";
import { randomId } from "./randomId";

describe("randomId", () => {
  it("is a v4 UUID, and does not repeat", () => {
    const ids = Array.from({ length: 1000 }, randomId);
    for (const id of ids) {
      expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    }
    expect(new Set(ids).size).toBe(ids.length);
  });
});
