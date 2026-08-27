/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** "mock" (default) serves from the in-browser mock backend; "live" talks to a real API. */
  readonly VITE_API_MODE?: "mock" | "live";
  /** Base URL of the real API, used when VITE_API_MODE is "live". */
  readonly VITE_API_BASE_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
