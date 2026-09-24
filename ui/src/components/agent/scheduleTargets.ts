import type { ScheduledRun } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";

/** The agent a schedule runs: a template and the harness it runs on. */
export interface SchedulePair {
  namespace: string;
  agentTemplate: string;
  harness: string;
}

/**
 * The schedules that run one agent.
 *
 * A `ScheduledRun` names a harness and an agent template, which is the same pair an
 * agent is — so a schedule belongs to an agent when both halves match. Both refs are
 * compared, not just the template: a template admitted by two harnesses is two
 * agents, and matching on the template alone would show each other's schedules.
 */
export function schedulesFor(
  schedules: readonly ScheduledRun[],
  pair: SchedulePair,
): ScheduledRun[] {
  return schedules.filter(
    (schedule) =>
      schedule.agentTemplate?.namespace === pair.namespace &&
      schedule.agentTemplate?.name === pair.agentTemplate &&
      schedule.harness?.namespace === pair.namespace &&
      schedule.harness?.name === pair.harness,
  );
}
