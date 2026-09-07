import type { ScheduledRunExecution } from "@/types";
import { mergeScheduledRunExecutions } from "@/lib/scheduledRuns";

function execution(
  id: string,
  startTime: string,
  status: ScheduledRunExecution["status"] = "InProgress",
): ScheduledRunExecution {
  return {
    id,
    startTime,
    trigger: "Scheduled",
    status,
  };
}

describe("mergeScheduledRunExecutions", () => {
  it("preserves the existing array when polling returns no changes", () => {
    const existing = [execution("one", "2026-08-03T10:00:00Z")];
    const incoming = [{ ...existing[0] }];

    expect(mergeScheduledRunExecutions(incoming, existing)).toBe(existing);
  });

  it("returns updated execution data when a status changes", () => {
    const existing = [execution("one", "2026-08-03T10:00:00Z")];
    const incoming = [execution("one", "2026-08-03T10:00:00Z", "Succeeded")];

    const result = mergeScheduledRunExecutions(incoming, existing);

    expect(result).not.toBe(existing);
    expect(result[0].status).toBe("Succeeded");
  });

  it("adds older pages while keeping newest executions first", () => {
    const existing = [execution("new", "2026-08-03T10:00:00Z")];
    const older = [execution("old", "2026-08-03T09:00:00Z", "Succeeded")];

    expect(mergeScheduledRunExecutions(older, existing).map(({ id }) => id)).toEqual([
      "new",
      "old",
    ]);
  });
});
