/**
 * Re-running the sign-in when the proxy's session has lapsed.
 *
 * `AuthStatus` has said "the UI should re-run OIDC" for `expired` since this module's
 * types were written, and nothing did. The header offered "Session expired — sign in",
 * which navigated to a page offering "Sign in with SSO" — two clicks to recover from
 * something the reader did not do, where the UI this replaced recovered on its own.
 *
 * ## Why a guard is not optional here
 *
 * The failure this must not cause is a redirect loop: leave for the proxy, come back
 * still `expired`, leave again. That happens for real — a proxy whose own cookie is
 * valid can keep handing back an id_token it will not refresh — and a loop is worse
 * than a stuck page because the reader cannot even read the error. So one attempt is
 * recorded, with a timestamp, and a second within the window declines to redirect and
 * leaves the buttons to do it manually.
 *
 * The window is wide enough for a slow identity provider round trip. Too narrow and a
 * genuinely in-flight re-auth reads as a loop; too wide and a reader who really did
 * lapse twice is refused a redirect they should have got.
 *
 * ## Why `unsecured` is not in this file
 *
 * There is no `/oauth2` endpoint in that state, so redirecting to one would bounce the
 * browser between the app and a 404 forever. The caller checks the status; this module
 * is only reached for `expired`.
 */

import { runtimeConfig } from "@/api/runtimeConfig";

/** Mirrors the key the Next UI used, so an in-flight attempt survives the swap. */
const ATTEMPT_KEY = "kagent_reauth_attempt";

/** Wide enough for a slow provider; short enough to still catch a fast loop. */
export const REAUTH_GUARD_WINDOW_MS = 60_000;

/** Where to come back to: the page being read, not the app's front door. */
function returnTo(location: Pick<Location, "pathname" | "search" | "hash">): string {
  return `${location.pathname}${location.search}${location.hash}`;
}

/**
 * The URL that restarts the flow and comes back to `target`.
 *
 * `rd` is oauth2-proxy's own parameter for it. Without it the proxy returns the reader
 * to whatever it defaults to, which is how signing in again used to cost somebody the
 * page they were reading.
 *
 * Takes the destination rather than reading `window.location`, because the sign-in page
 * is the one place where those differ: a reader who was sent there by the proxy is
 * *at* `/login`, and the page they wanted is in the query string. See `LoginPage`.
 */
export function ssoStartUrl(target: string): string {
  const start = runtimeConfig().ssoRedirectPath;
  return `${start}?rd=${encodeURIComponent(target)}`;
}

/** The URL that restarts the flow and comes back to the page being read. */
export function reauthenticationUrl(
  location: Pick<Location, "pathname" | "search" | "hash">,
): string {
  return ssoStartUrl(returnTo(location));
}

/**
 * Whether an attempt was made recently enough that another would be a loop.
 *
 * No recorded attempt means no loop, and that has to be said rather than fall out of the
 * arithmetic: defaulting the absent value to `0` made `now - 0` small for any small
 * clock, so the very first attempt read as a loop and declined to redirect. Real time
 * hid it — `Date.now()` is large enough that the subtraction was always over the window —
 * and it would have surfaced as re-authentication silently not happening.
 */
export function reauthenticationLooping(now = Date.now()): boolean {
  const raw = window.sessionStorage.getItem(ATTEMPT_KEY);
  if (raw === null) return false;

  const last = Number(raw);
  // An unparseable value is not evidence of anything, so it is not treated as an attempt.
  if (!Number.isFinite(last)) return false;

  return now - last < REAUTH_GUARD_WINDOW_MS;
}

/** Forgets the attempt, so a later lapse gets a redirect of its own. */
export function clearReauthenticationAttempt(): void {
  window.sessionStorage.removeItem(ATTEMPT_KEY);
}

/**
 * Leaves for the proxy, unless that would be a loop.
 *
 * Returns whether it went, so a caller can tell "recovering" from "stuck" — the latter
 * is what the manual buttons are for.
 *
 * `replace`, not `assign`: the page that could not authenticate is not somewhere Back
 * should return to, and leaving it in history means Back lands on a page that will
 * immediately try to leave again.
 */
export function startReauthentication(now = Date.now()): boolean {
  if (reauthenticationLooping(now)) return false;

  window.sessionStorage.setItem(ATTEMPT_KEY, String(now));
  window.location.replace(reauthenticationUrl(window.location));
  return true;
}
