import type { ResourceMetadata } from "./common";

export interface ScheduledRun {
  apiVersion?: string;
  kind?: "ScheduledRun";
  metadata: ResourceMetadata & { uid?: string };
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
// Takes the resource, or the provenance a conversation records for the schedule that
// started it.
export interface ScheduleIdentity { namespace: string; name: string; uid: string }

export function scheduleRouteID(schedule: ScheduledRun | ScheduleIdentity): string {
  const { namespace, name, uid } = "metadata" in schedule ? schedule.metadata : schedule;
  return `${namespace}:${name}:${uid}`;
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
