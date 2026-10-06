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

/** What a message says, or the names of the files it sent when it says nothing. */
export function messageSummary(message: ChatMessage): string {
  return (
    messageText(message) ||
    message.parts
      .flatMap((part) => (part.kind === "file" ? [part.name] : []))
      .join(", ")
  );
}
