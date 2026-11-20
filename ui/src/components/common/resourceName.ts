/**
 * Naming rules every create form shares.
 *
 * One copy on purpose: two forms with their own regex is how they end up
 * disagreeing about what a valid name is, and the user finds out from the
 * cluster rather than the field.
 */

/** A DNS-1123 label: what a namespace has to be, and the strictest name rule. */
export const DNS1123_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

/** A DNS-1123 subdomain: what the API accepts for an object name (`ResourceReference.name`). */
export const DNS1123_SUBDOMAIN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$/;
const OBJECT_NAME_MAX = 253;
const NAMESPACE_MAX = 63;

/** Lowercase letters, numbers, hyphens and dots, up to 253 characters. */
export function isValidObjectName(value: string): boolean {
  const trimmed = value.trim();
  return trimmed.length <= OBJECT_NAME_MAX && DNS1123_SUBDOMAIN.test(trimmed);
}

export function isValidNamespace(value: string): boolean {
  const trimmed = value.trim();
  return trimmed.length <= NAMESPACE_MAX && DNS1123_LABEL.test(trimmed);
}

/** The namespace a create form starts on when the user has not chosen one. */
export const DEFAULT_NAMESPACE = "kagent";

/** Wording used wherever a name fails the label rule, so the advice never varies. */
export const RESOURCE_NAME_HINT =
  "Use lowercase letters, numbers and hyphens, starting and ending with a letter or number.";

/** Object name hint, for fields that take a DNS-1123 subdomain. */
export const OBJECT_NAME_HINT =
  "Use lowercase letters, numbers, hyphens and dots, starting and ending with a letter or number.";

export function isValidResourceName(value: string): boolean {
  return DNS1123_LABEL.test(value.trim());
}

/**
 * Keeps typed text to a name's characters: lowercase, leading space dropped,
 * inner spaces as hyphens. Object names keep dots; namespaces and labels drop them.
 */
export function sanitizeName(value: string, allowDots: boolean): string {
  return value
    .toLowerCase()
    .replace(/^\s+/, "")
    .replace(/\s+/g, "-")
    .replace(allowDots ? /[^a-z0-9.-]/g : /[^a-z0-9-]/g, "");
}

/**
 * Turns arbitrary text into something that satisfies the rule.
 *
 * Used to suggest a name from a URL or a package, never to silently rewrite
 * what someone typed.
 */
export function slugifyResourceName(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
