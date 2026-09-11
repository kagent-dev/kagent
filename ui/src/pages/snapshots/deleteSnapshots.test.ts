import { afterEach, describe, expect, it, vi } from "vitest";
import { deleteSnapshots, deletedMessage } from "./deleteSnapshots";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("deleteSnapshots", () => {
  it("sends one request per snapshot, in order", async () => {
    const seen: string[] = [];

    const deleted = await deleteSnapshots(["a", "b", "c"], async (id) => {
      seen.push(id);
    });

    expect(seen).toEqual(["a", "b", "c"]);
    expect(deleted).toBe(3);
  });

  /*
   * The case the loop exists for: picking twelve rows and having the third refuse
   * should still remove the other eleven.
   */
  it("keeps going after one refuses, and still removes the rest", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const seen: string[] = [];

    const run = deleteSnapshots(["a", "b", "c"], async (id) => {
      seen.push(id);
      if (id === "b") throw new Error("still in use");
    });

    await expect(run).rejects.toThrow(/1 snapshot could not be deleted/);
    expect(seen).toEqual(["a", "b", "c"]);
  });

  it("rejects rather than reporting a partial run as a success", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});

    const run = deleteSnapshots(["a", "b"], async (id) => {
      if (id === "b") throw new Error("still in use");
    });

    // The count that did go is in the message, so the toast can say both halves.
    await expect(run).rejects.toThrow("1 deleted, 1 snapshot could not be deleted — still in use");
  });

  it("logs every failure, whatever the toast ends up saying", async () => {
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});

    await expect(
      deleteSnapshots(["a", "b"], async () => {
        throw new Error("nope");
      }),
    ).rejects.toThrow(/2 snapshots could not be deleted/);

    expect(logged).toHaveBeenCalledTimes(2);
  });

  it("carries the counts on the error, for a caller that wants them apart", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});

    await deleteSnapshots(["a", "b"], async (id) => {
      if (id === "b") throw new Error("still in use");
    }).catch((cause) => {
      expect(cause.deleted).toBe(1);
      expect(cause.failures).toEqual([{ id: "b", reason: "still in use" }]);
    });
  });
});

describe("deletedMessage", () => {
  it("counts one and many differently", () => {
    expect(deletedMessage(1)).toBe("Deleted 1 snapshot");
    expect(deletedMessage(4)).toBe("Deleted 4 snapshots");
  });
});
