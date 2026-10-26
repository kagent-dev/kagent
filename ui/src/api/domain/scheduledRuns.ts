import type { ResourceMetadata } from "./common";

export interface ScheduledRun {
  apiVersion?: string;
  kind?: "ScheduledRun";
  metadata: ResourceMetadata & { uid?: string; deletionTimestamp?: string };
  spec: {
    agentRef: { name: string };
    schedule: string;
    timeZone?: string;
    prompt: string;
    paused?: boolean;
    executionTimeout?: string;
  };
  status?: {
    observedGeneration?: number;
    conditions?: { type: string; status: string; reason?: string; message?: string }[];
  };
}

// A held history URL carries both the resource reference and its original UID.
// Kubernetes names exclude colons, so a recreated name cannot replace that UID.
export function scheduleRouteID(schedule: ScheduledRun): string {
  return `${schedule.metadata.namespace}:${schedule.metadata.name}:${schedule.metadata.uid}`;
}

export function scheduleRoute(id: string) {
  const [namespace = "", name = "", uid = ""] = id.split(":");
  return { ref: { namespace, name }, uid };
}

export function scheduleRef(schedule: ScheduledRun) {
  return { namespace: schedule.metadata.namespace ?? "", name: schedule.metadata.name };
}

export function scheduleDelete(schedule: ScheduledRun) {
  return { ref: scheduleRef(schedule), uid: schedule.metadata.uid ?? "", resourceVersion: schedule.metadata.resourceVersion ?? "" };
}
