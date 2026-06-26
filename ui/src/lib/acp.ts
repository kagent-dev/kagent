// Pure, side-effect-free helpers for the ACP harness chat. Kept separate from
// the hook so they can be unit-tested in isolation.

import type { ChatStatus } from "@/types";
import type { ConnState, SessionUpdate } from "@/types/acp";

/** Strip the "[Working directory: ...]" prefix some backends prepend to titles. */
export function cleanSessionTitle(title: string): string {
  return title.replace(/^\s*\[Working directory:[^\]]*\]\s*/i, "").trim();
}

/** Extract the text of an ACP content block (`{ text: string }`), if present. */
export function chunkText(content: unknown): string {
  if (content && typeof content === "object" && "text" in content) {
    const text = (content as { text?: unknown }).text;
    return typeof text === "string" ? text : "";
  }
  return "";
}

/** Derive the displayable text of a completed/failed tool call update. */
export function toolResultText(update: SessionUpdate): string {
  if (Array.isArray(update.content)) {
    const parts = (update.content as { content?: { text?: string } }[])
      .map((c) => c?.content?.text ?? "")
      .filter(Boolean);
    if (parts.length > 0) return parts.join("\n");
  }
  if (update.rawOutput !== undefined) {
    return typeof update.rawOutput === "string" ? update.rawOutput : JSON.stringify(update.rawOutput, null, 2);
  }
  return "";
}

/** Map the ACP connection state onto the shared chat status vocabulary. */
export function connToChatStatus(conn: ConnState): ChatStatus {
  switch (conn) {
    case "idle":
    case "ready":
      return "ready";
    case "running":
      return "working";
    case "disconnected":
      return "error";
    default:
      return "thinking";
  }
}
