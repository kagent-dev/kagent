import { useState } from "react";

/** The note beside a submit button that invalid fields hold back. */
export const FIX_FIELDS_MESSAGE = "Fill in the required (*) fields.";

/** One problem with a draft, tied to the field that shows it. */
export interface FieldIssue<F extends string = string> {
  field: F;
  message: string;
}

/** What a `FormField` takes from validation: its message and a way to mark it touched. */
export interface FieldValidation {
  error?: string;
  onTouch: () => void;
}

/** A default `fieldFor` for forms rendered without validation, such as read-only views. */
export const noValidation = (): FieldValidation => ({ onTouch: () => {} });

/** The first message for `field`, if any. */
export function issueFor<F extends string>(issues: readonly FieldIssue<F>[], field: F): string | undefined {
  return issues.find((issue) => issue.field === field)?.message;
}

/**
 * Inline validation: a field shows its issue once touched (left, or given a value),
 * and every field shows its issue after a blocked submit, such as Enter in a field.
 */
export function useSubmitValidation<F extends string>(issues: readonly FieldIssue<F>[]) {
  const [submitted, setSubmitted] = useState(false);
  const [touched, setTouched] = useState<ReadonlySet<F>>(() => new Set());
  const shown = issues.filter((issue) => submitted || touched.has(issue.field));
  const touch = (field: F) => setTouched((prev) => (prev.has(field) ? prev : new Set(prev).add(field)));

  return {
    submitted,
    /** True when the draft has no issues, shown or not. */
    valid: issues.length === 0,
    /** The issues to display: those on touched fields, or all after a blocked submit. */
    issues: shown,
    /** True when the draft can be sent; otherwise shows every issue. */
    attempt: () => {
      if (issues.length > 0) setSubmitted(true);
      return issues.length === 0;
    },
    errorFor: (field: F) => issueFor(shown, field),
    /** Props for a `FormField`: its shown error and the touch callback. */
    field: (field: F): FieldValidation => ({ error: issueFor(shown, field), onTouch: () => touch(field) }),
    touch,
    reset: () => {
      setSubmitted(false);
      setTouched(new Set());
    },
  };
}
