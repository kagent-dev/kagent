import { invoke } from "@/api/operations";
import { useApiResource } from "@/api/hooks/useApiResource";
import { schedulesFor, type ScheduledAgentRef } from "./scheduleTargets";

/** The schedules that target this agent, read once and narrowed in the browser. */
export function useAgentSchedules(agent: ScheduledAgentRef) {
  // `ListScheduledRuns` takes only a page, so the narrowing is done here. Past 100
  // schedules that needs a server-side filter on the RPC, not a second page read.
  const schedules = useApiResource(["scheduledRuns.list", "agents"], () =>
    invoke("scheduledRuns.list", { page: { limit: 100 } }),
  );
  return { schedules, rows: schedules.data ? schedulesFor(schedules.data.scheduledRuns ?? [], agent) : undefined };
}
