import { isValidSessionTimestamp, mergeSessionUpdate, normalizeSessionTimestamps } from "@/lib/sessionTimestamps";
import type { Session } from "@/types";

const baseSession: Session = {
  id: "sess-1",
  name: "Hello",
  agent_id: "agent-1",
  user_id: "user-1",
  created_at: "2026-06-08T12:00:00.000Z",
  updated_at: "2026-06-08T12:05:00.000Z",
  deleted_at: "",
};

describe("sessionTimestamps", () => {
  it("rejects Go zero timestamps", () => {
    expect(isValidSessionTimestamp("0001-01-01T00:00:00Z")).toBe(false);
    expect(isValidSessionTimestamp(undefined)).toBe(false);
  });

  it("accepts real timestamps", () => {
    expect(isValidSessionTimestamp("2026-06-08T12:00:00.000Z")).toBe(true);
  });

  it("fills missing timestamps when normalizing", () => {
    const fallback = new Date("2026-06-08T15:00:00.000Z");
    const normalized = normalizeSessionTimestamps(
      { ...baseSession, created_at: "0001-01-01T00:00:00Z", updated_at: "0001-01-01T00:00:00Z" },
      fallback,
    );
    expect(normalized.created_at).toBe("2026-06-08T15:00:00.000Z");
    expect(normalized.updated_at).toBe("2026-06-08T15:00:00.000Z");
  });

  it("preserves existing timestamps when merging title updates", () => {
    const merged = mergeSessionUpdate(baseSession, {
      ...baseSession,
      name: "Updated title",
      created_at: "0001-01-01T00:00:00Z",
      updated_at: "0001-01-01T00:00:00Z",
    });
    expect(merged.name).toBe("Updated title");
    expect(merged.created_at).toBe(baseSession.created_at);
    expect(merged.updated_at).toBe(baseSession.updated_at);
  });
});
