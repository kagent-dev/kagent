/**
 * A conversation's name, from the message that started it.
 *
 * A conversation is created by its first message, so that message is the only thing
 * available to name it — and it is a better name than anything a form could ask for.
 * Every row reading "New conversation" made the list of conversations useless as a
 * list.
 *
 * Its own module because both the application's chat page and an installed
 * distribution's need the same answer, and a list where the two disagreed about how a
 * conversation is named would be worse than either convention alone.
 */

/** Long enough for a real question, short enough for one line of a narrow rail. */
export const CONVERSATION_NAME_LIMIT = 60;

export function conversationName(firstMessage: string): string {
  // The first line only: a pasted stack trace would otherwise make a row as tall as
  // the rail, and the first line of one is the useful part anyway.
  const firstLine = firstMessage.trim().split("\n")[0]?.trim() ?? "";
  if (firstLine === "") return "New conversation";

  return firstLine.length > CONVERSATION_NAME_LIMIT
    ? `${firstLine.slice(0, CONVERSATION_NAME_LIMIT).trimEnd()}…`
    : firstLine;
}
