/**
 * The three states the UI has always modelled, carried over from the Next
 * server action this replaces.
 *
 * - `authenticated` — a valid session; the user's identity is known.
 * - `expired`       — an auth proxy is in front and answering, but the session
 *                     it holds is no longer good. The UI should re-run OIDC.
 * - `unsecured`     — there is no auth proxy in front at all.
 *
 * `unsecured` is the one with teeth. The original code carried a warning about
 * it and it still applies: the UI must never redirect in this state, because
 * the `/oauth2` endpoint it would redirect to does not exist, and the browser
 * would bounce between the app and a 404 forever. Every ambiguous outcome
 * resolves here, so the loop is unreachable by construction rather than by
 * remembering to check.
 */
export type AuthStatus = "authenticated" | "expired" | "unsecured";

/** Who is signed in, normalised across whichever source resolved it. */
export interface AuthUser {
  /** Stable identifier — the proxy's subject, falling back to email. */
  id: string;
  /** Best available label for the header. Never empty. */
  displayName: string;
  email?: string;
  groups?: string[];
}

export interface AuthResult {
  status: AuthStatus;
  /** Non-null only when `status` is `authenticated`. */
  user: AuthUser | null;
}

/**
 * Where authentication state comes from.
 *
 * The seam exists because the endpoint is a decision the deployment may
 * revisit: today it is oauth2-proxy's `/oauth2/userinfo`, which works against
 * the chart as it stands; a backend `/api/me` would need Go changes to the
 * controller. Swapping is writing one more implementation of this interface and
 * changing which one `AuthProvider` is given — no consumer moves.
 */
export interface AuthSource {
  /** Identifies the implementation in diagnostics. */
  readonly id: string;
  /**
   * Resolves the current session.
   *
   * Must never reject. A transport failure is not an error condition here — it
   * is evidence that nothing is fronting the app, which is `unsecured`.
   */
  resolve(signal?: AbortSignal): Promise<AuthResult>;
}

/** The `unsecured` result, which is also the safe answer to anything unclear. */
export const UNSECURED: AuthResult = { status: "unsecured", user: null };
