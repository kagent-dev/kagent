import type { ScheduledRun, ScheduledRunExecution } from "@/types";

function executionsEqual(
  left: ScheduledRunExecution,
  right: ScheduledRunExecution,
): boolean {
  return left.id === right.id
    && left.startTime === right.startTime
    && left.completionTime === right.completionTime
    && left.trigger === right.trigger
    && left.sessionID === right.sessionID
    && left.taskID === right.taskID
    && left.status === right.status
    && left.statusMessage === right.statusMessage;
}

export function mergeScheduledRunExecutions(
  incoming: ScheduledRunExecution[],
  existing: ScheduledRunExecution[],
): ScheduledRunExecution[] {
  const merged = new Map<string, ScheduledRunExecution>();
  for (const execution of [...incoming, ...existing]) {
    if (!merged.has(execution.id)) merged.set(execution.id, execution);
  }

  const next = [...merged.values()].sort(
    (left, right) => new Date(right.startTime).getTime() - new Date(left.startTime).getTime(),
  );
  if (
    next.length === existing.length
    && next.every((execution, index) => executionsEqual(execution, existing[index]))
  ) {
    return existing;
  }
  return next;
}

export function isFailedExecutionStatus(status: string | undefined): boolean {
  return status === "DispatchFailed" || status === "Failed" || status === "TimedOut";
}

export function scheduledRunTargetNamespace(sr: ScheduledRun): string {
  return sr.metadata.namespace || "";
}

export function formatScheduledRunTargetRef(sr: ScheduledRun): string {
  const targetRef = sr.spec.targetRef;
  const namespace = scheduledRunTargetNamespace(sr);
  const ref = namespace ? `${namespace}/${targetRef.name}` : targetRef.name;
  return `${targetRef.kind} ${ref}`;
}

export function scheduledRunDetailPath(namespace: string, name: string): string {
  return `/schedules/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`;
}

export function scheduledRunEditPath(namespace: string, name: string): string {
  return `/schedules/new?${new URLSearchParams({
    edit: "true",
    name,
    namespace,
  }).toString()}`;
}

export function getScheduledRunDisplayStatus(sr: ScheduledRun) {
  const accepted = sr.status?.conditions?.find((condition) => condition.type === "Accepted");
  if (accepted?.status === "False") {
    return {
      label: accepted.reason || "Rejected",
      variant: "destructive" as const,
      className: "",
      title: accepted.message,
    };
  }
  if (sr.spec.suspended) {
    return {
      label: "Suspended",
      variant: "secondary" as const,
      className: "",
      title: undefined,
    };
  }
  if (!accepted || accepted.status === "Unknown") {
    return {
      label: "Pending",
      variant: "outline" as const,
      className: "text-amber-600 border-amber-600",
      title: accepted?.message,
    };
  }
  return {
    label: "Active",
    variant: "outline" as const,
    className: "text-green-600 border-green-600",
    title: accepted.message,
  };
}
