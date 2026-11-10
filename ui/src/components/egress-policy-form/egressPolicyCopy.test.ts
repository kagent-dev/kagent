import { describe, expect, it } from "vitest";
import { egressCopy } from "./egressPolicyCopy";

describe("deleteWarning", () => {
  it("leaves the generic warning when nothing uses the policy or the count is unknown", () => {
    expect(egressCopy.deleteWarning(0)).toBeUndefined();
    expect(egressCopy.deleteWarning(undefined)).toBeUndefined();
  });

  it("names one user in the singular", () => {
    expect(egressCopy.deleteWarning(1)).toBe(
      "This policy is in use by 1 agent or sandbox template you can see. It will immediately lose access to the hosts it allows. This action cannot be undone.",
    );
  });

  it("counts several users", () => {
    expect(egressCopy.deleteWarning(3)).toBe(
      "This policy is in use by 3 agents and sandbox templates you can see. They will immediately lose access to the hosts it allows. This action cannot be undone.",
    );
  });
});
