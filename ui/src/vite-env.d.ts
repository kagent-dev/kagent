/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Optional build-time override; normal development uses runtime configuration. */
  readonly KAGENT_UI_VITE_API_MODE?: "mock" | "live";
  /**
   * `"true"` installs the bundled Example App Extension.
   *
   * A switch for that one config, not a list of extensions to install: an
   * extension has to be imported to be in the bundle at all, so which ones are
   * installed is the array in `appExtensions/activeExtensions.ts`.
   */
  readonly KAGENT_UI_VITE_EXAMPLE_EXTENSION?: "true" | "false";
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
