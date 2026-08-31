/**
 * Deployment configuration, in the shapes the app actually consumes.
 *
 * A thin derivation over `@/env`, which owns *how* settings reach the browser.
 * This module owns *what they mean*: the numeric timeout is validated here so a
 * bad value cannot reach the chat client, and every reader gets the coerced form
 * rather than a string it has to parse for itself.
 *
 * Separate from `api/config.ts` on purpose: that file answers "which backend is
 * this build talking to". This one answers "what did the operator configure".
 */

import { ENV_DEFAULTS, env } from "@/env";

export interface RuntimeConfig {
  /** Where "Sign in with SSO" sends the browser. */
  ssoRedirectPath: string;
  /** Chat stream inactivity timeout before the client aborts. */
  streamTimeoutMs: number;
}

/** What the app runs on when nothing is configured. */
export const RUNTIME_CONFIG_DEFAULTS: RuntimeConfig = {
  ssoRedirectPath: ENV_DEFAULTS.SSO_REDIRECT_PATH,
  streamTimeoutMs: Number(ENV_DEFAULTS.STREAM_TIMEOUT_MS),
};

/**
 * The resolved configuration.
 *
 * Read on every call rather than captured once, so a test can change the
 * environment between cases without reloading modules. The values behind it do
 * not change during a page's life, so this is not a reactivity concern.
 */
export function runtimeConfig(): RuntimeConfig {
  return {
    ssoRedirectPath: env("SSO_REDIRECT_PATH"),
    streamTimeoutMs: streamTimeoutMs(),
  };
}

/**
 * The timeout, or the default when what was configured is not a usable number.
 *
 * A non-numeric or non-positive value would otherwise abort every chat stream
 * immediately — a failure that looks like the backend hanging up, which is the
 * hardest kind of misconfiguration to trace back to its cause.
 */
function streamTimeoutMs(): number {
  const parsed = Number(env("STREAM_TIMEOUT_MS"));
  return Number.isFinite(parsed) && parsed > 0
    ? parsed
    : RUNTIME_CONFIG_DEFAULTS.streamTimeoutMs;
}
