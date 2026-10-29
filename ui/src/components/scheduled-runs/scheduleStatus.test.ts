import { describe, expect, it } from "vitest";
import type { ScheduledRun } from "@/api/domain/scheduledRuns";
import { scheduleStatus } from "./scheduleStatus";

function schedule(paused: boolean, conditions?: NonNullable<ScheduledRun["status"]>["conditions"]): ScheduledRun {
  return {
    metadata: { namespace: "team", name: "daily" },
    spec: { agentRef: { name: "report" }, schedule: "0 9 * * *", prompt: "Report", paused },
    status: conditions ? { conditions } : undefined,
  };
}

describe("scheduleStatus", () => {
  it.each([
    ["a ready schedule is active", schedule(false, [{ type: "Ready", status: "True", reason: "Reconciled" }]), { kind: "active" }],
    ["paused wins over readiness", schedule(true, [{ type: "Ready", status: "False", reason: "InvalidConfiguration" }]), { kind: "paused" }],
    ["no condition is not reported, not active", schedule(false), { kind: "notReported" }],
    ["an unrelated condition is not Ready", schedule(false, [{ type: "Other", status: "True" }]), { kind: "notReported" }],
    ["Ready False carries the controller's words", schedule(false, [{ type: "Ready", status: "False", reason: "AgentNotFound", message: "Agent does not exist" }]),
      { kind: "notReady", reason: "AgentNotFound", message: "Agent does not exist" }],
    ["Ready Unknown is not ready", schedule(false, [{ type: "Ready", status: "Unknown", reason: "SchedulerNotConfigured" }]),
      { kind: "notReady", reason: "SchedulerNotConfigured", message: undefined }],
  ])("%s", (_, input, expected) => {
    expect(scheduleStatus(input)).toEqual(expected);
  });
});
