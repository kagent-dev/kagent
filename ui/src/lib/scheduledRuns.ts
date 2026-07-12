import type { ScheduledRun, ScheduledRunTargetKind } from "@/types";

export function scheduledRunTargetKind(sr: ScheduledRun): ScheduledRunTargetKind {
  return sr.spec.targetRef.kind;
}

export function formatScheduledRunTargetRef(sr: ScheduledRun): string {
  const targetRef = sr.spec.targetRef;
  const namespace = sr.metadata.namespace || "";
  const ref = namespace ? `${namespace}/${targetRef.name}` : targetRef.name;
  return `${ref} (${scheduledRunTargetKind(sr)})`;
}

export function getScheduledRunAcceptedCondition(sr: ScheduledRun) {
  return sr.status?.conditions?.find((condition) => condition.type === "Accepted");
}

export function getScheduledRunDisplayStatus(sr: ScheduledRun) {
  const accepted = getScheduledRunAcceptedCondition(sr);
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

export function hasPendingRunHistory(sr: ScheduledRun): boolean {
  return Boolean(
    sr.status?.runHistory?.some((entry) => entry.status === "Pending")
  );
}
