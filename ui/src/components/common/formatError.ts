/**
 * An error as raw text: the message, and the stack when there is one.
 *
 * Shared by the boundaries, which show it behind a disclosure. Kept verbatim —
 * a stack is only readable with its own line breaks and indentation.
 */
export function formatError(error: unknown): string {
  if (error instanceof Error) {
    return error.stack?.includes(error.message)
      ? error.stack
      : `${error.name}: ${error.message}\n${error.stack ?? ""}`.trimEnd();
  }
  return String(error);
}
