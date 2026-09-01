import { UNSECURED } from "./types";
import type { AuthResult, AuthSource, AuthUser } from "./types";

/** oauth2-proxy's userinfo endpoint, relative to the app's own origin. */
const DEFAULT_USERINFO_PATH = "/oauth2/userinfo";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function firstString(
  source: Record<string, unknown>,
  keys: readonly string[],
): string | undefined {
  for (const key of keys) {
    const value = source[key];
    if (typeof value === "string" && value.length > 0) return value;
  }
  return undefined;
}

function stringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const items = value.filter((entry): entry is string => typeof entry === "string");
  return items.length > 0 ? items : undefined;
}

/**
 * Maps a userinfo document onto `AuthUser`.
 *
 * Key names are read leniently: oauth2-proxy has used `user` and
 * `preferredUsername`, OIDC claims use `sub` and `preferred_username`, and a
 * future `/api/me` may use something else again. Returns null when the document
 * identifies nobody, which is treated as "not a real session".
 */
export function toAuthUser(payload: unknown): AuthUser | null {
  if (!isRecord(payload)) return null;

  const id = firstString(payload, ["user", "sub", "userID", "id", "email"]);
  const email = firstString(payload, ["email"]);
  const displayName = firstString(payload, [
    "preferredUsername",
    "preferred_username",
    "name",
    "user",
    "email",
  ]);

  if (id === undefined) return null;

  return {
    id,
    displayName: displayName ?? id,
    email,
    groups: stringArray(payload.groups),
  };
}

/**
 * Classifies a userinfo response.
 *
 * Exported for unit testing, because the interesting behaviour is entirely in
 * which response maps to which state, and that is far cheaper to pin down here
 * than through the UI.
 */
export async function classifyUserInfoResponse(
  response: Response,
): Promise<AuthResult> {
  // Only an explicit rejection from a proxy that is genuinely in front counts
  // as an expired session. Nothing else may produce `expired`, which is what
  // keeps the redirect path unreachable when no proxy exists.
  if (response.status === 401 || response.status === 403) {
    return { status: "expired", user: null };
  }

  if (!response.ok) return UNSECURED;

  // A static SPA answers unknown paths with index.html, so "no auth proxy"
  // arrives as 200 text/html rather than a 404. Without this check the app
  // would hand an HTML page to JSON.parse and, worse, a malformed-but-parsable
  // body could read as a valid session.
  if (!response.headers.get("content-type")?.includes("json")) {
    return UNSECURED;
  }

  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    return UNSECURED;
  }

  const user = toAuthUser(payload);
  return user ? { status: "authenticated", user } : UNSECURED;
}

export interface OAuth2ProxyAuthSourceOptions {
  /** Override the userinfo path if the proxy is mounted elsewhere. */
  userInfoPath?: string;
  /** Injectable for tests. */
  fetchImpl?: typeof fetch;
}

/**
 * Reads the session from oauth2-proxy.
 *
 * Chosen over a backend `/api/me` because it works against the deployment as it
 * stands — `/api/me` would need controller changes outside this branch. The
 * `AuthSource` interface is what makes that a later, cheap decision.
 */
export function createOAuth2ProxyAuthSource(
  options: OAuth2ProxyAuthSourceOptions = {},
): AuthSource {
  const path = options.userInfoPath ?? DEFAULT_USERINFO_PATH;
  const doFetch = options.fetchImpl ?? globalThis.fetch.bind(globalThis);

  return {
    id: `oauth2-proxy(${path})`,
    async resolve(signal) {
      try {
        const response = await doFetch(path, {
          signal,
          cache: "no-store",
          // Deliberately no `Accept: application/json`. oauth2-proxy answers
          // with JSON regardless, and asking for it changes what a server
          // *without* the proxy does: a static SPA host returns its 200
          // index.html for an unknown path under the default `*/*`, but 404s
          // when JSON is demanded. The 200-HTML shape is the real unsecured
          // response in production, and a 404 additionally makes the browser
          // log a console error for a condition that is not an error. Taking
          // the default keeps every no-proxy environment on the same path.
          credentials: "same-origin",
        });
        return await classifyUserInfoResponse(response);
      } catch {
        // A network error means nothing answered. That is not a fault to
        // surface — it is the ordinary state of an app running with no proxy.
        return UNSECURED;
      }
    },
  };
}
