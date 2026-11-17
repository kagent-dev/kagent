import { useState } from "react";

/** The `ValidationNotice` heading when a submit is stopped by invalid fields. */
export const FIX_FIELDS_TITLE = "Nothing was sent yet";

/** The `ValidationNotice` text when a submit is stopped by invalid fields. */
export const FIX_FIELDS_MESSAGE = "Fix the highlighted fields and try again.";

/** One problem with a draft, tied to the field that shows it. */
export interface FieldIssue<F extends string = string> {
  field: F;
  message: string;
}

/** The first message for `field`, if any. */
export function issueFor<F extends string>(issues: readonly FieldIssue<F>[], field: F): string | undefined {
  return issues.find((issue) => issue.field === field)?.message;
}

/**
 * Inline validation on submit: nothing is flagged until the first submit,
 * then every field shows its current issue as the user fixes it.
 */
export function useSubmitValidation<F extends string>(issues: readonly FieldIssue<F>[]) {
  const [submitted, setSubmitted] = useState(false);
  const shown = submitted ? issues : [];

  return {
    submitted,
    /** The issues to display: none before the first submit. */
    issues: shown,
    /** Marks the form submitted; true when the draft can be sent. */
    attempt: () => {
      setSubmitted(true);
      return issues.length === 0;
    },
    errorFor: (field: F) => issueFor(shown, field),
    reset: () => setSubmitted(false),
  };
}
