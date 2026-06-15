import type { Session } from "@/types";

/** Go's zero time serializes as 0001-01-01, which browsers display as "Dec 31". */
export function isValidSessionTimestamp(value?: string | null): boolean {
  if (!value) {
    return false;
  }
  const ts = Date.parse(value);
  return !Number.isNaN(ts) && ts >= Date.UTC(2020, 0, 1);
}

export function normalizeSessionTimestamps(session: Session, fallback = new Date()): Session {
  const iso = fallback.toISOString();
  return {
    ...session,
    created_at: isValidSessionTimestamp(session.created_at) ? session.created_at : iso,
    updated_at: isValidSessionTimestamp(session.updated_at)
      ? session.updated_at
      : isValidSessionTimestamp(session.created_at)
        ? session.created_at
        : iso,
  };
}

export function mergeSessionUpdate(existing: Session, incoming: Session): Session {
  return normalizeSessionTimestamps(
    {
      ...existing,
      ...incoming,
      created_at: isValidSessionTimestamp(incoming.created_at) ? incoming.created_at : existing.created_at,
      updated_at: isValidSessionTimestamp(incoming.updated_at) ? incoming.updated_at : existing.updated_at,
    },
    new Date(existing.updated_at || existing.created_at || Date.now()),
  );
}
