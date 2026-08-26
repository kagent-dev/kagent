/**
 * Which way the mock backend should behave right now.
 *
 * Loading, empty and failure are the states a UI gets wrong, and they are the
 * ones a happy-path fixture never shows. Rather than editing handlers to see
 * them, the scenario is read from the URL on every request: `/agents?mock=error`
 * renders the error state, `?mock=empty` the empty state, and going back to
 * `/agents` restores the normal data — no reload, no rebuild.
 *
 * It is also persisted, so `?mock=slow` survives an in-app navigation that drops
 * the query string. `?mock=ok` clears it again.
 */

export const MOCK_SCENARIOS = ["ok", "empty", "error", "slow"] as const;

export type MockScenario = (typeof MOCK_SCENARIOS)[number];

/** Query parameter and storage key the scenario is read from. */
export const MOCK_SCENARIO_PARAM = "mock";
const STORAGE_KEY = "kagent.mockScenario";

/** How long the mock backend waits before answering, per scenario. */
export const SCENARIO_DELAY_MS: Record<MockScenario, number> = {
  // Long enough that a loading state is genuinely observable, short enough that
  // it does not feel broken.
  ok: 450,
  empty: 450,
  error: 450,
  slow: 2_500,
};

/** The scenario in force, re-read on every request so it can change mid-session. */
export function currentScenario(): MockScenario {
  const fromUrl = readFromUrl();
  if (fromUrl) {
    remember(fromUrl);
    return fromUrl;
  }
  return readFromStorage() ?? "ok";
}

function readFromUrl(): MockScenario | null {
  if (typeof window === "undefined") return null;
  const value = new URLSearchParams(window.location.search).get(MOCK_SCENARIO_PARAM);
  return isScenario(value) ? value : null;
}

function readFromStorage(): MockScenario | null {
  try {
    const value = window.localStorage.getItem(STORAGE_KEY);
    return isScenario(value) ? value : null;
  } catch {
    // Storage can be blocked outright; the default scenario is a fine answer.
    return null;
  }
}

function remember(scenario: MockScenario): void {
  try {
    if (scenario === "ok") window.localStorage.removeItem(STORAGE_KEY);
    else window.localStorage.setItem(STORAGE_KEY, scenario);
  } catch {
    // Not being able to persist only costs the scenario a navigation.
  }
}

function isScenario(value: string | null): value is MockScenario {
  return value !== null && (MOCK_SCENARIOS as readonly string[]).includes(value);
}

// ---------------------------------------------------------------------------
// Chat transport
// ---------------------------------------------------------------------------

/**
 * How the mock chat transport should behave.
 *
 * Kept separate from the REST scenario above because the two fail
 * independently: a conversation can break while the session list loads fine, and
 * that combination is exactly what the chat error states are for. Folding them
 * into one switch would make "the stream failed" untestable without also taking
 * out the page around it.
 *
 * `asks` is the turn that ends in a question. The agent calls a tool that asks the
 * reader something and its turn parks in `input_required` — which is not a failure
 * and not a completion, holds the instance's one active-task slot, and makes the
 * controller refuse every further message until it is answered or given up. It is
 * its own value rather than a variation of `ok` because the state persists in the
 * conversation: a reload lands back in it, which is the case worth driving.
 *
 * `asks-text` is the same parked turn asking a different shape of question, and it is
 * a scenario of its own because the shape decides the controls: `asks` offers choices,
 * `asks-text` offers one prose field — the only shape where the field can take the
 * caret on arrival and Enter can mean "send".
 */
export const CHAT_SCENARIOS = ["ok", "error", "slow", "asks", "asks-text"] as const;

export type ChatScenario = (typeof CHAT_SCENARIOS)[number];

export const CHAT_SCENARIO_PARAM = "chat";

/** The chat scenario in force, re-read per turn so it can change mid-session. */
export function currentChatScenario(): ChatScenario {
  if (typeof window === "undefined") return "ok";
  const value = new URLSearchParams(window.location.search).get(CHAT_SCENARIO_PARAM);
  return isChatScenario(value) ? value : "ok";
}

function isChatScenario(value: string | null): value is ChatScenario {
  return value !== null && (CHAT_SCENARIOS as readonly string[]).includes(value);
}

/**
 * Which authentication state the mock backend answers `/oauth2/userinfo` with.
 *
 * A third axis, separate from the two above for the same reason they are separate from
 * each other: whether a proxy is in front is independent of whether the API is answering,
 * and the combination that matters most — a healthy API behind a lapsed session — is
 * unreachable if the two are one switch.
 *
 * Defaults to `unsecured`, which is both what mock mode should say and what it has always
 * said: there is no backend to have signed in to, so reporting anybody would be a
 * fabrication. The other two exist so the states a deployment actually has can be driven
 * from a laptop — see `playwright/tests/auth/auth-modes.spec.ts`.
 */
export const AUTH_SCENARIOS = ["unsecured", "authenticated", "expired"] as const;

export type AuthScenario = (typeof AUTH_SCENARIOS)[number];

export const AUTH_SCENARIO_PARAM = "auth";

const AUTH_STORAGE_KEY = "kagent.mockAuthScenario";

/**
 * The auth scenario in force, re-read per request so it can change mid-session.
 *
 * Remembered like the REST scenario, and for a sharper reason: an expired session makes
 * the app *leave* for the proxy, and the URL it lands on carries no `?auth=`. Read from
 * the URL alone, the state would evaporate on the one navigation the state exists to
 * cause — and the redirect guard, which only matters on the second visit, would be
 * untestable.
 */
export function currentAuthScenario(): AuthScenario {
  if (typeof window === "undefined") return "unsecured";

  const fromUrl = new URLSearchParams(window.location.search).get(
    AUTH_SCENARIO_PARAM,
  );
  if (isAuthScenario(fromUrl)) {
    rememberAuth(fromUrl);
    return fromUrl;
  }

  try {
    const stored = window.localStorage.getItem(AUTH_STORAGE_KEY);
    if (isAuthScenario(stored)) return stored;
  } catch {
    // Storage can be blocked outright; unsecured is a fine answer.
  }

  return "unsecured";
}

function rememberAuth(scenario: AuthScenario): void {
  try {
    if (scenario === "unsecured") window.localStorage.removeItem(AUTH_STORAGE_KEY);
    else window.localStorage.setItem(AUTH_STORAGE_KEY, scenario);
  } catch {
    // Not being able to persist only costs the scenario a navigation.
  }
}

function isAuthScenario(value: string | null): value is AuthScenario {
  return value !== null && (AUTH_SCENARIOS as readonly string[]).includes(value);
}
