import { useEffect, useRef, useState } from "react";

/** The note beside a submit button that invalid fields hold back. */
export const FIX_FIELDS_MESSAGE = "Fill in the required (*) fields.";

/** One problem with a draft, tied to the field that shows it. */
export interface FieldIssue<F extends string = string> {
  field: F;
  message: string;
}

/** What a `FormField` takes from validation: its message, and ways to mark it left or typed in. */
export interface FieldValidation {
  error?: string;
  onTouch: () => void;
  /** Called on each keystroke with the field's value. */
  onType?: (value: string) => void;
}

/** How long typing must pause before a new message shows. */
export const TYPING_PAUSE_MS = 200;

/** A default `fieldFor` for forms rendered without validation, such as read-only views. */
export const noValidation = (): FieldValidation => ({ onTouch: () => {} });

/** The first message for `field`, if any. */
export function issueFor<F extends string>(issues: readonly FieldIssue<F>[], field: F): string | undefined {
  return issues.find((issue) => issue.field === field)?.message;
}

/**
 * Inline validation: a field shows its issue once touched (left, or given a value),
 * and every field shows its issue after a blocked submit, such as Enter in a field.
 * While typing, new messages wait for a pause so the form does not jump on each key;
 * leaving a field or submitting shows them at once.
 */
export function useSubmitValidation<F extends string>(issues: readonly FieldIssue<F>[]) {
  const [submitted, setSubmitted] = useState(false);
  const [touched, setTouched] = useState<ReadonlySet<F>>(() => new Set());
  // The messages on screen when typing started; null when not typing.
  const [held, setHeld] = useState<readonly FieldIssue<F>[] | null>(null);
  const pause = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(pause.current), []);

  const due = issues.filter((issue) => submitted || touched.has(issue.field));
  const shown = held
    ? due.filter((issue) => held.some((kept) => kept.field === issue.field && kept.message === issue.message))
    : due;
  const settle = () => {
    clearTimeout(pause.current);
    setHeld(null);
  };
  const touch = (field: F) => {
    settle();
    setTouched((prev) => (prev.has(field) ? prev : new Set(prev).add(field)));
  };
  const type = (field: F, value: string) => {
    if (!held) setHeld(shown);
    clearTimeout(pause.current);
    pause.current = setTimeout(() => setHeld(null), TYPING_PAUSE_MS);
    if (value.trim() !== "") setTouched((prev) => (prev.has(field) ? prev : new Set(prev).add(field)));
  };

  return {
    submitted,
    /** True when the draft has no issues, shown or not. */
    valid: issues.length === 0,
    /** The issues to display: those on touched fields, or all after a blocked submit. */
    issues: shown,
    /** True when the draft can be sent; otherwise shows every issue. */
    attempt: () => {
      settle();
      if (issues.length > 0) setSubmitted(true);
      return issues.length === 0;
    },
    errorFor: (field: F) => issueFor(shown, field),
    /** Props for a `FormField`: its shown error and the touch and typing callbacks. */
    field: (field: F): FieldValidation => ({
      error: issueFor(shown, field),
      onTouch: () => touch(field),
      onType: (value: string) => type(field, value),
    }),
    touch,
    reset: () => {
      settle();
      setSubmitted(false);
      setTouched(new Set());
    },
  };
}
