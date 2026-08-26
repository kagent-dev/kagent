import { defineConfig } from "vitest/config";
import { loadEnv, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { CORE_ENV_KEYS, ENV_DEFAULTS } from "./src/env.ts";

/**
 * Where the dev server forwards `/api` and `/a2a`.
 *
 * Read from `.env` as well as the shell, which takes a `loadEnv` rather than a
 * `process.env` lookup: Vite does not put `.env` values on `process.env`, so a
 * setting read only from there is one a reader can put in their `.env` and watch do
 * nothing. `.env.example` ships this pointed at the UI pod's nginx on 8080, which is
 * the forward `dev-scripts/setup-cluster.sh` already holds open.
 *
 * The shell still wins, for the same reason it wins over the inlined settings below:
 * a one-off override on the command line should not mean editing a file.
 *
 * The fallback is the controller's own port, for a checkout with no `.env` at all.
 */
function devProxy(mode: string) {
  const fromFiles = loadEnv(mode, import.meta.dirname, "");
  const target =
    process.env.KAGENT_DEV_CONTROLLER_URL ||
    fromFiles.KAGENT_DEV_CONTROLLER_URL ||
    "http://127.0.0.1:8083";

  return {
    "/api": { target, changeOrigin: true },
    "/a2a": { target, changeOrigin: true },
  };
}

/**
 * What an extension's settings are named, rather than what they are.
 *
 * An installed extension reads settings this application knows nothing about, and a
 * developer still has to be able to set them in `.env`. This used to be a list of the
 * three the extension happened to want, which meant a public repository enumerating
 * another product's configuration and a change here every time that product grew a
 * setting — for keys nothing in this repository reads.
 *
 * A prefix instead. `readEnv` already takes any key and `window.environmentVariables`
 * is already an open record, so nothing in the application changes when an extension
 * adds a setting: it names it `EXTENSION_SOMETHING` and it arrives.
 *
 * Still bounded, which is the point of `CORE_ENV_KEYS` being a list at all: the dev
 * server inlines these into the document, so a wholesale copy of the environment would
 * publish every credential on the machine into the HTML. A variable has to be
 * deliberately named for an extension to be copied — but only that deliberately, so a
 * stray `EXTENSION_` variable in a shell will be inlined. It is a development server
 * rendering a page for the developer who started it; the container path below is the
 * one that faces a cluster, and there the environment is the chart's own.
 */
const EXTENSION_ENV_PREFIX = "EXTENSION_";

/**
 * Serves the deployment configuration the way the container does.
 *
 * In production a startup script renders `public/env-config.js` from the pod's
 * environment. There is no such step in front of the dev server, so this plugin
 * replaces the tag that loads it with an inline copy built from `.env` and the
 * shell. Inline rather than a served file so it cannot be cached, and so the
 * values are unmistakably present in the document when debugging.
 */
function devEnvConfig(mode: string): Plugin {
  return {
    name: "dev-env-config",
    apply: "serve",

    transformIndexHtml(html) {
      // `""` as the prefix is what makes this read keys without Vite's own
      // `VITE_` convention, which is the point: these are the same names the
      // chart sets, not bundler variables.
      const fromFiles = loadEnv(mode, import.meta.dirname, "");
      const merged: Record<string, string> = { ...ENV_DEFAULTS };

      // The shell wins over `.env`, so a one-off override on the command line
      // does not require editing a file that is then easy to forget. Both sources are
      // searched for the extension prefix, so an extension's setting can be put in
      // either, exactly like one of the application's own.
      const extensionKeys = [
        ...Object.keys(fromFiles),
        ...Object.keys(process.env),
      ].filter((key) => key.startsWith(EXTENSION_ENV_PREFIX));

      for (const key of [...CORE_ENV_KEYS, ...extensionKeys]) {
        const value = process.env[key] ?? fromFiles[key];
        if (value !== undefined && value !== "") merged[key] = value;
      }

      // Escaping `<` keeps a value containing markup from closing this script
      // element early, which would otherwise inject whatever followed it.
      const serialised = JSON.stringify(merged, null, 2).replace(/</g, "\\u003c");

      return html.replace(
        /<script[^>]+src="[^"]*\/env-config\.js"[^>]*><\/script>/,
        `<script>\nwindow.environmentVariables = ${serialised};\n</script>`,
      );
    },

    configureServer(server) {
      // A changed `.env` has to reach the page the same way it would reach a
      // restarted pod: the values are read once at load, so nothing short of a
      // full reload picks them up.
      server.watcher.add(path.resolve(import.meta.dirname, ".env"));
      server.watcher.on("change", (file) => {
        if (file.endsWith(".env")) {
          server.ws.send({ type: "full-reload", path: "*" });
        }
      });
    },
  };
}

export default defineConfig(({ mode }) => ({
  // JSX is transformed by oxc, which routes the factory at @emotion/react —
  // that alone enables the `css` prop, no Babel step required.
  plugins: [react({ jsxImportSource: "@emotion/react" }), devEnvConfig(mode)],
  // The component library's bundle assigns to `global` at module top level,
  // which exists in Node and not in a browser. Referencing an undeclared
  // identifier throws before `global ?? window` can fall back, so importing
  // anything from the library takes the whole page down. The library ships one
  // barrel with no per-component entry points, so this cannot be avoided by
  // importing more narrowly.
  define: { global: "globalThis" },
  optimizeDeps: {
    // Dev pre-bundles dependencies before serving them, and that pass does not see the
    // top-level `define` above — so it needs its own copy.
    //
    // `rolldownOptions`, not `esbuildOptions`: Vite 8 pre-bundles with Rolldown and
    // deprecated the esbuild form, which it warned about on every start. Worse than the
    // noise, a deprecated option that stops being read would take the shim with it
    // silently — and the failure that produces is the component library taking the whole
    // page down, a long way from this line.
    // Nested under `transform`, which is where rolldown takes a define map.
    rolldownOptions: { transform: { define: { global: "globalThis" } } },
  },
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "./src") },
  },
  server: {
    port: Number(process.env.UI_LOOP_PORT ?? 8001),
    host: "0.0.0.0",
    // Stands in for what nginx does in a deployed cluster, so a dev server
    // talking to a real controller uses the same relative URLs as production.
    // Requests are only proxied in live mode; the mock worker intercepts first
    // otherwise, so these rules are inert by default.
    proxy: devProxy(mode),
  },
  preview: {
    port: Number(process.env.UI_LOOP_PORT ?? 8001),
    host: "0.0.0.0",
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/testSetup.ts"],
    css: true,
    exclude: ["**/node_modules/**", "**/playwright/**"],
    // Unit tests drive the client against this repo's own mock handlers, so the
    // mode is stated rather than inherited. It used to come free from the dev
    // default; that default is gone, because a page that quietly serves fixtures
    // when the backend is down is worse than one that says so.
    env: { VITE_API_MODE: "mock" },
  },
}));
