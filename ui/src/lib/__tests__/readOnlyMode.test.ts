import { afterAll, beforeEach, describe, expect, it, jest } from "@jest/globals";

describe("readOnlyMode", () => {
  const originalEnv = process.env;

  beforeEach(() => {
    jest.resetModules();
    process.env = { ...originalEnv };
  });

  afterAll(() => {
    process.env = originalEnv;
  });

  it("enables read-only mode for truthy env values", async () => {
    process.env.NEXT_PUBLIC_READONLY_MODE = "TRUE";

    const { isReadOnlyModeEnabled } = await import("../readOnlyMode");

    expect(isReadOnlyModeEnabled()).toBe(true);
  });

  it("disables read-only mode by default", async () => {
    delete process.env.NEXT_PUBLIC_READONLY_MODE;

    const { isReadOnlyModeEnabled } = await import("../readOnlyMode");

    expect(isReadOnlyModeEnabled()).toBe(false);
  });

  it("creates a BaseResponse error payload for blocked actions", async () => {
    const { createReadOnlyModeResponse } = await import("../readOnlyMode");

    expect(createReadOnlyModeResponse("Agent changes")).toEqual({
      message: "Agent changes are disabled because this UI is running in read-only mode.",
      error: "Agent changes are disabled because this UI is running in read-only mode.",
    });
  });
});
