/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** "mock" (default) serves from the in-browser mock backend; "live" talks to a real API. */
  readonly VITE_API_MODE?: "mock" | "live";
  /** Base URL of the real API, used when VITE_API_MODE is "live". */
  readonly VITE_API_BASE_URL?: string;
  /**
   * `"true"` installs the bundled Example App Extension.
   *
   * A switch for that one config, not a list of extensions to install: an
   * extension has to be imported to be in the bundle at all, so which ones are
   * installed is the array in `appExtensions/activeExtensions.ts`.
   */
  readonly VITE_EXAMPLE_EXTENSION?: "true" | "false";
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
