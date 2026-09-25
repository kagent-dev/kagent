import type { ScheduledRun } from "@/generated/kagent/api/v1alpha1/scheduled_runs_pb";
export interface ScheduledAgentRef { namespace: string; name: string; }
export function schedulesFor(schedules: readonly ScheduledRun[], agent: ScheduledAgentRef): ScheduledRun[] {
 return schedules.filter(schedule => schedule.agent?.namespace === agent.namespace && schedule.agent?.name === agent.name);
}
