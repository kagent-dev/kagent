import type { ScheduledRun } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";
export interface SchedulePair { namespace: string; name: string; }
export function schedulesFor(schedules: readonly ScheduledRun[], agent: SchedulePair): ScheduledRun[] {
 return schedules.filter(schedule => schedule.agent?.namespace === agent.namespace && schedule.agent?.name === agent.name);
}
