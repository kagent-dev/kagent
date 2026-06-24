/** Default title used when a session is provisioned before the first message. */
export const PLACEHOLDER_SESSION_TITLE = "Chat";

export function deriveSessionTitle(message: string): string {
  const trimmed = message.trim();
  if (!trimmed) {
    return "";
  }
  return trimmed.slice(0, 20) + (trimmed.length > 20 ? "..." : "");
}

export function isPlaceholderSessionTitle(name?: string | null): boolean {
  if (!name?.trim()) {
    return true;
  }
  return name.trim().toLowerCase() === PLACEHOLDER_SESSION_TITLE.toLowerCase();
}
