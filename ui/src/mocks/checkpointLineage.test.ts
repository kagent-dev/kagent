import { beforeEach, expect, test } from "vitest";
import {
  deleteCheckpoint,
  readCheckpoints,
  recordCheckpointFork,
  renameCheckpoint,
  saveCheckpoint,
  SEEDED_CHECKPOINT,
} from "./state";

beforeEach(() => window.sessionStorage.clear());

test("forks list their inherited prefix with original provenance, including later saves at that boundary", () => {
  const source = SEEDED_CHECKPOINT;
  saveCheckpoint({ ...source, id: "after-cutoff", headTaskId: "later-task" });
  recordCheckpointFork(source.agentInstanceId, "fork", [source.headTaskId]);
  expect(readCheckpoints("fork")).toEqual([source]);

  const laterSave = saveCheckpoint({ ...source, id: "saved-after-fork" });
  const local = saveCheckpoint({ ...source, id: "fork-local", agentInstanceId: "fork", headTaskId: "fork-task" });
  recordCheckpointFork("fork", "nested", [source.headTaskId, local.headTaskId]);
  expect(readCheckpoints("nested")).toEqual(
    expect.arrayContaining([source, laterSave, local]),
  );
  expect(readCheckpoints("nested")).toHaveLength(3);
  expect(readCheckpoints("unrelated")).toEqual([]);

  renameCheckpoint(source.id, "Renamed at the source");
  expect(readCheckpoints("nested").find((row) => row.id === source.id)).toMatchObject({
    name: "Renamed at the source", agentInstanceId: source.agentInstanceId,
  });
});

test("retained prefixes block deletion but checkpoints after the cutoff remain deletable", () => {
  const source = SEEDED_CHECKPOINT;
  recordCheckpointFork(source.agentInstanceId, "fork", [source.headTaskId]);
  expect(deleteCheckpoint(source.id)).toBe(false);
  expect(readCheckpoints("fork")).toEqual([source]);

  const later = saveCheckpoint({ ...source, id: "after-cutoff", headTaskId: "later-task" });
  expect(deleteCheckpoint(later.id)).toBe(true);
  expect(readCheckpoints(source.agentInstanceId)).toEqual([source]);
});
