// Any fixed placeholder works here -- it's never dereferenced, just used as
// the base for URL parsing so we can tell whether `rd` stayed same-origin.
const SENTINEL_ORIGIN = "http://kagent-login-redirect.invalid";

/**
 * Validate a post-login redirect target.
 *
 * Only a same-origin relative path is safe to hand back to oauth2-proxy's
 * `rd` parameter: an absolute URL, a protocol-relative `//host/...`, or a
 * disguised variant of either (e.g. a backslash or a stripped tab/newline
 * that the URL Standard normalizes into one of the above) would let a
 * crafted `/login?rd=...` link send an authenticated session off to an
 * attacker's site after sign-in.
 */
export function sanitizeRedirect(rd: string | undefined): string {
  if (!rd) return "/";
  try {
    const url = new URL(rd, SENTINEL_ORIGIN);
    if (url.origin !== SENTINEL_ORIGIN || url.pathname.startsWith("//")) {
      return "/";
    }
    return url.pathname + url.search + url.hash;
  } catch {
    return "/";
  }
}
