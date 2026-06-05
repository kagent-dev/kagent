/**
 * Maps substrate / A2A proxy errors to user-facing chat messages.
 */
export function formatA2AClientError(raw: string): string {
  const lower = raw.toLowerCase();
  if (
    lower.includes("no free workers") ||
    lower.includes("substrate worker pool has no free workers")
  ) {
    return "All substrate workers are busy. Close another chat, wait for a session to finish, or increase WorkerPool replicas, then try again.";
  }
  if (raw.trim()) {
    return raw.trim();
  }
  return "Request failed";
}
