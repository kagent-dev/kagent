import { Alert, Button } from "antd";
import { isApiError } from "@/api";

interface SubmitErrorProps {
  /** Whatever the mutation rejected with. */
  error: unknown;
  /** What the user was trying to do, e.g. "MCP server". */
  what: string;
  /**
   * Which write failed, as the word the message ends on.
   *
   * Every form that saves changes to something that already exists shares this
   * component, and telling that user their resource "was not created" describes a
   * write nobody attempted — worse, it reads as though the thing they were editing
   * might now be gone.
   */
  outcome?: "created" | "saved";
  onRetry: () => void;
  "data-testid"?: string;
}

/**
 * A failed create or save, explained in terms the user can act on.
 *
 * A rejected write is not the same as a failed read: the user has unsaved input
 * on screen, so the message has to say plainly that nothing was written before
 * they navigate away assuming it was — in the words of the write they actually
 * attempted, which is what `outcome` selects.
 */
export function SubmitError({
  error,
  what,
  outcome = "created",
  onRetry,
  "data-testid": testId,
}: SubmitErrorProps) {
  return (
    <Alert
      type="error"
      showIcon
      title={`The ${what} was not ${outcome}`}
      data-testid={testId}
      description={explain(error, outcome)}
      action={
        <Button size="small" onClick={onRetry}>
          Try again
        </Button>
      }
    />
  );
}

/**
 * The most useful sentence available for a rejection.
 *
 * The status codes worth their own wording are the ones the user can do
 * something about — a name collision or a rejected payload are fixable here,
 * where a 500 is not.
 */
function explain(error: unknown, outcome: "created" | "saved"): string {
  if (!isApiError(error)) {
    return error instanceof Error ? error.message : "Something went wrong.";
  }
  if (error.isUnreachable) {
    return `The backend could not be reached, so nothing was ${outcome}. Check the connection and try again.`;
  }
  if (error.status === 409) {
    return "Something with that name already exists in this namespace. Choose a different name.";
  }
  if (error.status === 400 || error.status === 422) {
    return `The backend rejected the request: ${error.message}`;
  }
  return error.message;
}
