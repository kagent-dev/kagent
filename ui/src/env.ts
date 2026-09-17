/**
 * Deployment settings that reach the browser at runtime.
 *
 * `import.meta.env` is inlined by the bundler at build time, so anything an
 * operator configures per deployment cannot travel in the bundle — it would
 * freeze the chart's values as of the image build and silently ignore whatever
 * was actually set. These values arrive instead on `window.environmentVariables`,
 * written by the container on every start (`scripts/init.sh`) and loaded by a
 * plain script tag ahead of the app, so they are readable synchronously by the
 * first module that runs.
 *
 * Names are shared with the surrounding platform's own frontend where that
 * platform has an equivalent setting, so one chart value configures both and
 * neither needs a translation layer.
 */

/** Every setting the application itself reads. */
export interface EnvironmentVariables {
  /** Base URL the browser calls the API on. */
  API_BASE_URL: string;
  /** Where "Sign in with SSO" sends the browser. */
  SSO_REDIRECT_PATH: string;
  /** Chat stream inactivity timeout, in milliseconds, before the client aborts. */
  STREAM_TIMEOUT_MS: string;
  /** `"true"` serves the whole API from in-browser fixtures. */
  ENABLE_MOCK_UI: string;
}

/**
 * The keys the dev server is allowed to copy out of the shell.
 *
 * A whitelist rather than "everything the process has", because the dev server
 * inlines these into the page: passing the environment through wholesale would
 * publish every credential on the developer's machine into the HTML.
 *
 * An extension's own keys are not listed here, and are not listed anywhere: anything
 * named `EXTENSION_*` is passed through by the dev server and by `scripts/init.sh`,
 * and read back with `readEnv`, which takes any key. This application does not know
 * what an extension's settings are and does not need to.
 */
export const CORE_ENV_KEYS = [
  "API_BASE_URL",
  "SSO_REDIRECT_PATH",
  "STREAM_TIMEOUT_MS",
  "ENABLE_MOCK_UI",
] as const satisfies readonly (keyof EnvironmentVariables)[];

/**
 * What the app uses when a key is absent — the normal case for `yarn dev` and
 * the e2e suite, neither of which serves a rendered `env-config.js`. Values match
 * the chart defaults, so a developer sees what a default deployment does.
 */
export const ENV_DEFAULTS: EnvironmentVariables = {
  API_BASE_URL: "/api",
  SSO_REDIRECT_PATH: "/oauth2/start",
  STREAM_TIMEOUT_MS: "1800000",
  ENABLE_MOCK_UI: "false",
};

declare global {
  interface Window {
    environmentVariables?: Record<string, string | undefined>;
  }
}

function raw(): Record<string, string | undefined> {
  // Guarded rather than assumed: unit tests import the modules that read this
  // without any document having loaded a script tag.
  return typeof window === "undefined" ? {} : (window.environmentVariables ?? {});
}

/**
 * One setting, falling back to its default when unset or empty.
 *
 * Per-key rather than merging the whole object once, so a document written by an
 * older image that predates a key still contributes the keys it does have.
 */
export function env<K extends keyof EnvironmentVariables>(key: K): string {
  const value = raw()[key];
  return typeof value === "string" && value.length > 0 ? value : ENV_DEFAULTS[key];
}

/**
 * A setting the application has no opinion about, for an extension that ships
 * its own.
 *
 * Untyped and defaulted by the caller: an extension's settings are not the
 * application's business, and listing them in `EnvironmentVariables` would make
 * the core type grow every time somebody installs something. The extension is
 * also responsible for getting its key into the container's environment.
 */
export function readEnv(key: string, fallback = ""): string {
  const value = raw()[key];
  return typeof value === "string" && value.length > 0 ? value : fallback;
}

/**
 * Whether a setting reads as on.
 *
 * `includes` rather than equality because these values are shell strings that
 * pass through Helm, and `"true\n"` should not read as off.
 */
export function envFlag(key: keyof EnvironmentVariables): boolean {
  return env(key).toString().toLowerCase().includes("true");
}

/**
 * Whether a setting was configured at all, as opposed to falling back.
 *
 * The distinction matters for a flag whose default depends on something else:
 * "explicitly off" and "unset" are different instructions, and `envFlag` alone
 * reports both as false. An empty string counts as unset, since that is how an
 * unset chart value renders.
 */
export function envIsSet(key: keyof EnvironmentVariables): boolean {
  const value = raw()[key];
  return typeof value === "string" && value.length > 0;
}
