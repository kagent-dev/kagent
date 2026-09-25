import type { ChatMessage } from "@/api";

/** A message's prose, with structured parts left out. */
export function messageText(message: ChatMessage): string {
  return message.parts
    .filter((part) => part.kind === "text")
    .map((part) => part.text)
    .join("");
}

/** True when nothing has arrived for this message yet — a stream about to fill in. */
export function isAwaitingContent(message: ChatMessage): boolean {
  return message.parts.every((part) => part.kind === "text" && part.text === "");
}
