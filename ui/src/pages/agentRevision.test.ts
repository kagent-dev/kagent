import { describe, expect, it } from "vitest";
import { agentRevisionCondition } from "./agentRevision";

describe("agent template revision conditions", () => {
  it("surfaces a compiler compatibility failure even when Ready was never set", () => {
    const compatible = {
      type: "Compatible",
      status: "False",
      reason: "UnsupportedConfiguration",
      message: 'Harness "codex" does not support structured output',
    };

    expect(
      agentRevisionCondition([
        { type: "Accepted", status: "True" },
        { type: "ResolvedRefs", status: "True" },
        compatible,
      ]),
    ).toBe(compatible);
  });

  it("uses Ready after every earlier stage succeeds", () => {
    const ready = { type: "Ready", status: "True", reason: "Ready" };
    expect(
      agentRevisionCondition([
        { type: "Accepted", status: "True" },
        { type: "Compatible", status: "True" },
        ready,
      ]),
    ).toBe(ready);
  });
});
