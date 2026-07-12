import type { ScheduledRun } from "@/types";

export function scheduledRunTargetNamespace(sr: ScheduledRun): string {
  return sr.spec.targetRef.namespace || sr.metadata.namespace || "";
}

export function formatScheduledRunTargetRef(sr: ScheduledRun): string {
  const targetRef = sr.spec.targetRef;
  const namespace = scheduledRunTargetNamespace(sr);
  const ref = namespace ? `${namespace}/${targetRef.name}` : targetRef.name;
  return `${ref} (${targetRef.kind})`;
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
  if (sr.spec.suspend) {
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

export function hasInProgressRunHistory(sr: ScheduledRun): boolean {
  return Boolean(
    sr.status?.runHistory?.some((entry) => entry.status === "InProgress")
  );
}
