import type { Agent } from "@/types";

/**
 * Short English relative time (e.g. "2h ago", "3d ago"). Falls back to a
 * short absolute date for older items. Returns "—" when the date is missing/invalid.
 */
export function formatTimeAgoShort(iso: string | undefined, nowMs: number = Date.now()): string {
  if (iso == null || iso === "") {
    return "—";
  }
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) {
    return "—";
  }
  const diffSec = Math.max(0, Math.floor((nowMs - t) / 1000));
  if (diffSec < 60) {
    return "just now";
  }
  if (diffSec < 3600) {
    return `${Math.floor(diffSec / 60)}m ago`;
  }
  if (diffSec < 86400) {
    return `${Math.floor(diffSec / 3600)}h ago`;
  }
  if (diffSec < 604800) {
    return `${Math.floor(diffSec / 86400)}d ago`;
  }
  return new Date(t).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/** Latest time across status condition transitions; falls back to `creationTimestamp` when no conditions. */
export function getLastActivityTimeIso(agent: Agent): string | undefined {
  const conds = agent.status?.conditions;
  let best = 0;
  if (conds) {
    for (const c of conds) {
      const x = c.lastTransitionTime;
      if (x) {
        const ms = new Date(x).getTime();
        if (!Number.isNaN(ms) && ms > best) {
          best = ms;
        }
      }
    }
  }
  if (best > 0) {
    return new Date(best).toISOString();
  }
  return agent.metadata.creationTimestamp;
}
