/**
 * Naming rules every create form shares.
 *
 * One copy on purpose: two forms with their own regex is how they end up
 * disagreeing about what a valid name is, and the user finds out from the
 * cluster rather than the field.
 */

/** A Kubernetes RFC-1123 subdomain — what a resource name has to be. */
export const RFC1123_SUBDOMAIN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

/** The namespace a create form starts on when the user has not chosen one. */
export const DEFAULT_NAMESPACE = "kagent";

/** Wording used wherever a name fails the rule, so the advice never varies. */
export const RESOURCE_NAME_HINT =
  "Use lowercase letters, numbers and hyphens, starting and ending with a letter or number.";

export function isValidResourceName(value: string): boolean {
  return RFC1123_SUBDOMAIN.test(value.trim());
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
