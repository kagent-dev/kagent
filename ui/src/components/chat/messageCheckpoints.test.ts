import { describe, expect, it } from "vitest";
import type { Checkpoint, ChatMessage } from "@/api";
import { checkpointsByMessage, groupByCheckpoint } from "./messageCheckpoints";

const said = (id: string, role: ChatMessage["role"], taskId?: string): ChatMessage => ({
  id,
  role,
  parts: [{ kind: "text", text: id }],
  createdAt: "2025-01-01T00:00:00Z",
  taskId,
});

const saved = (
  id: string,
  headTaskId: string,
  state: Checkpoint["state"] = "ready",
): Checkpoint => ({ id, agentInstanceId: "instance", headTaskId, state });

const TRANSCRIPT = [
  said("m1", "user", "task-1"),
  said("m2", "agent", "task-1"),
  said("m3", "user", "task-2"),
  said("m4", "agent", "task-2"),
];

describe("checkpointsByMessage", () => {
  it("covers the whole turn a checkpoint was taken at, question and answer", () => {
    const marks = checkpointsByMessage(TRANSCRIPT, [saved("c1", "task-1")], new Map());

    expect([...marks]).toEqual([
      ["m1", "c1"],
      ["m2", "c1"],
    ]);
  });

  it("covers every turn that has a checkpoint, not only the newest", () => {
    const marks = checkpointsByMessage(
      TRANSCRIPT,
      [saved("c1", "task-1"), saved("c2", "task-2")],
      new Map(),
    );

    expect([...marks.values()]).toEqual(["c1", "c1", "c2", "c2"]);
  });

  it("ignores a checkpoint that is not ready to fork from", () => {
    const marks = checkpointsByMessage(
      TRANSCRIPT,
      [saved("c1", "task-1", "failed")],
      new Map(),
    );

    expect(marks.size).toBe(0);
  });

  /*
   * The case the second source exists for: the reader's newest message is the
   * optimistic copy put on screen when they pressed send, and it has no task id until
   * the transcript is read back.
   */
  it("covers a message saved on this page that has no turn yet, and its reply", () => {
    const pending = [...TRANSCRIPT, said("m5", "user"), said("m6", "agent")];

    const marks = checkpointsByMessage(pending, [], new Map([["m5", "c9"]]));

    expect([...marks]).toEqual([
      ["m5", "c9"],
      ["m6", "c9"],
    ]);
  });

  it("stops at the next thing the reader says", () => {
    const pending = [...TRANSCRIPT, said("m5", "user"), said("m6", "user")];

    const marks = checkpointsByMessage(pending, [], new Map([["m5", "c9"]]));

    expect(marks.has("m6")).toBe(false);
  });

  it("forgets a message saved on this page once it is no longer in the transcript", () => {
    const marks = checkpointsByMessage(TRANSCRIPT, [], new Map([["gone", "c9"]]));

    expect(marks.size).toBe(0);
  });
});

describe("groupByCheckpoint", () => {
  it("draws one group per checkpointed turn, and one per message otherwise", () => {
    const marks = checkpointsByMessage(TRANSCRIPT, [saved("c1", "task-1")], new Map());

    const groups = groupByCheckpoint(TRANSCRIPT, marks);

    expect(
      groups.map((group) => [group.checkpointId, group.messages.map((m) => m.id)]),
    ).toEqual([
      ["c1", ["m1", "m2"]],
      [undefined, ["m3"]],
      [undefined, ["m4"]],
    ]);
  });

  it("keeps two adjacent boundaries apart", () => {
    const marks = checkpointsByMessage(
      TRANSCRIPT,
      [saved("c1", "task-1"), saved("c2", "task-2")],
      new Map(),
    );

    expect(groupByCheckpoint(TRANSCRIPT, marks).map((g) => g.checkpointId)).toEqual([
      "c1",
      "c2",
    ]);
  });
});
