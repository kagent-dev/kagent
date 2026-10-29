import type { ScheduledRun } from "@/api/domain/scheduledRuns";

/**
 * Whether a schedule will fire, as the controller last reported it.
 *
 * `spec.paused` alone is what the reader asked for, not what is happening: a schedule
 * whose Agent is missing, or whose scheduler is not configured, is not paused and still
 * never fires. The controller says so on the Ready condition, so that decides the rest.
 */
export type ScheduleStatus =
  | { kind: "paused" }
  | { kind: "active" }
  | { kind: "notReady"; reason?: string; message?: string }
  | { kind: "notReported" };

export function scheduleStatus(schedule: ScheduledRun): ScheduleStatus {
  if (schedule.spec.paused) return { kind: "paused" };
  const ready = schedule.status?.conditions?.find((condition) => condition.type === "Ready");
  if (!ready) return { kind: "notReported" };
  if (ready.status === "True") return { kind: "active" };
  return { kind: "notReady", reason: ready.reason || undefined, message: ready.message || undefined };
}
