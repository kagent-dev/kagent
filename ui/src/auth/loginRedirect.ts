/**
 * Validating the destination oauth2-proxy hands back to the sign-in page.
 *
 * An unauthenticated request to `/agents/foo` is answered by oauth2-proxy's
 * `sign_in.html`, which forwards to `/login?rd=%2Fagents%2Ffoo`. That `rd` is
 * attacker-controllable — a crafted `/login?rd=...` link is a URL anybody can
 * send — and it is handed straight back to the proxy as the place to land after
 * a successful sign-in. So it is checked here before it is used.
 */

// Any fixed placeholder works: it is never dereferenced, only used as the base
// for URL parsing so we can tell whether `rd` stayed same-origin.
const SENTINEL_ORIGIN = "http://kagent-login-redirect.invalid";

/**
 * The `rd` value if it is a same-origin path, `/` otherwise.
 *
 * Only a same-origin relative path is safe to return to. An absolute URL, a
 * protocol-relative `//host/...`, or a disguised variant of either — a
 * backslash, or a tab the URL Standard strips before parsing — would send an
 * authenticated session off to somebody else's site the moment sign-in
 * completed.
 *
 * The `//` check is on the *parsed* path rather than the input, because `.` and
 * `..` segments are resolved away first: `/.//evil.example.com` is same-origin
 * to the parser and normalizes to `//evil.example.com`, which is protocol-
 * relative again by the time anything else reads it.
 */
export function sanitizeRedirect(rd: string | null | undefined): string {
  if (!rd) return "/";
  try {
    const url = new URL(rd, SENTINEL_ORIGIN);
    if (url.origin !== SENTINEL_ORIGIN || url.pathname.startsWith("//")) {
      return "/";
    }
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return "/";
  }
}
