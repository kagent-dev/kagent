import type { ComponentType } from "react";

/**
 * Forms that accept extension-contributed fields. Same naming shape as extension
 * point IDs, minus the slot segment — the form itself is the target.
 */
export const EXTENSION_FORM_IDS = [
  "app_agents_agentNew_agentForm",
  "app_models_modelNew_modelForm",
  "app_mcpServers_mcpServerNew_mcpServerForm",
] as const;

export type ExtensionFormId = (typeof EXTENSION_FORM_IDS)[number];

const EXTENSION_FORM_ID_SET: ReadonlySet<string> = new Set(EXTENSION_FORM_IDS);

export function isExtensionFormId(value: string): value is ExtensionFormId {
  return EXTENSION_FORM_ID_SET.has(value);
}

/** The request body a form is building. Extensions widen it with their own keys. */
export type ExtensionFormPayload = Record<string, unknown>;

/** Props handed to an extension's field renderer. */
export interface ExtensionFormFieldProps<TValue = unknown> {
  /** The contribution's `id`, for `htmlFor`/`aria-describedby` wiring. */
  id: string;
  value: TValue;
  onChange: (next: TValue) => void;
  /** Message from `validate`, if the field is currently invalid. */
  error?: string;
  /** True while the form is submitting, so the field can disable itself. */
  disabled: boolean;
}

/**
 * An extension field: its inputs, its outputs, and its renderer.
 *
 * The two payload mappers are the point of the contract. Extension CRDs differ,
 * so a field cannot assume its value lives at its own name in the request —
 * `toPayload` writes it wherever that extension's API expects, and `fromPayload`
 * reads it back when an existing resource is loaded for editing.
 */
export interface ExtensionFormFieldContribution<
  TValue = unknown,
  TPayload extends ExtensionFormPayload = ExtensionFormPayload,
> {
  /** Unique within the form. Also the DOM id of the rendered field. */
  id: string;
  formId: ExtensionFormId;
  /** Lower sorts first, among extension fields only. */
  order?: number;
  /** The full renderer. The framework supplies no input of its own. */
  Component: ComponentType<ExtensionFormFieldProps<TValue>>;
  /** Value used when the form opens blank. */
  defaultValue: TValue;
  /** Reads the field's value out of an existing payload, for edit flows. */
  fromPayload: (payload: TPayload) => TValue;
  /** Merges the field's value into the outgoing payload. */
  toPayload: (payload: TPayload, value: TValue) => TPayload;
  /** Returns a message when the value is invalid, `undefined` when it is fine. */
  validate?: (value: TValue) => string | undefined;
}

/**
 * Type-checks a field against its own value and payload types, then erases the
 * generics so heterogeneous fields can share one array.
 *
 * The erasure is the reason this helper exists: `TValue` sits in both a return
 * position (`fromPayload`) and a parameter position (`toPayload`), so a
 * `ExtensionFormFieldContribution<string>` is not assignable to
 * `ExtensionFormFieldContribution<unknown>`. Authors get full inference inside the
 * call; the framework treats the values opaquely and only ever round-trips them
 * between `Component` and the two mappers, which agree on the real type.
 */
export function defineExtensionFormField<
  TValue,
  TPayload extends ExtensionFormPayload = ExtensionFormPayload,
>(
  field: ExtensionFormFieldContribution<TValue, TPayload>,
): ExtensionFormFieldContribution {
  return field as ExtensionFormFieldContribution;
}

/** The extension fields for one form, in render order. */
export function extensionFieldsForForm(
  fields: readonly ExtensionFormFieldContribution[] | undefined,
  formId: ExtensionFormId,
): ExtensionFormFieldContribution[] {
  return (fields ?? [])
    .filter((field) => field.formId === formId)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
}

/** Starting values for a blank form, keyed by contribution id. */
export function initialExtensionFieldValues(
  fields: readonly ExtensionFormFieldContribution[],
): Record<string, unknown> {
  return Object.fromEntries(fields.map((field) => [field.id, field.defaultValue]));
}

/**
 * Hydrates extension field values from a payload being edited.
 *
 * Generic in the payload for the same reason as `applyExtensionFieldValues`: a
 * caller holding a typed request should not have to widen it to hand it over.
 */
export function readExtensionFieldValues<TPayload extends object>(
  fields: readonly ExtensionFormFieldContribution[],
  payload: TPayload,
): Record<string, unknown> {
  return Object.fromEntries(
    fields.map((field) => [
      field.id,
      field.fromPayload(payload as ExtensionFormPayload),
    ]),
  );
}

/**
 * Folds every extension field's value into the request payload, in order.
 *
 * Returns the caller's own payload type rather than the open record the fields
 * are written against. The fields treat the payload opaquely — that is what
 * lets one form host contributions from extensions whose CRDs disagree — but a
 * page building a typed request should get that type back, not have to assert
 * its way out of an erasure the framework introduced. The two casts here are
 * the price, paid once, so no call site pays it.
 */
export function applyExtensionFieldValues<TPayload extends object>(
  fields: readonly ExtensionFormFieldContribution[],
  payload: TPayload,
  values: Record<string, unknown>,
): TPayload {
  return fields.reduce<TPayload>(
    (next, field) =>
      field.toPayload(next as ExtensionFormPayload, values[field.id]) as TPayload,
    payload,
  );
}

/** Validation messages keyed by contribution id; empty when the form is valid. */
export function validateExtensionFieldValues(
  fields: readonly ExtensionFormFieldContribution[],
  values: Record<string, unknown>,
): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const field of fields) {
    const message = field.validate?.(values[field.id]);
    if (message !== undefined) errors[field.id] = message;
  }
  return errors;
}
