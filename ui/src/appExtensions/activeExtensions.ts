import type { AppExtensionConfig } from "./types";
import { exampleAppExtension } from "./example/exampleExtension";

/**
 * The extensions this build installs, in order.
 *
 * Installing one is two lines: import its config above, and add it to this array.
 * Everything it contributes arrives through the extension points, so no other
 * module has to know it exists — this file is the only one an install touches.
 *
 * **Order is the precedence.** Additive contributions from every entry all take
 * effect, in this order; where two extensions say something about the same single
 * thing — the sidebar component, the document title, an operation's implementation —
 * the later entry wins. So list the extension whose opinion should prevail last.
 *
 * The worked example is not installed by default: it is documentation you can run,
 * not a feature of the application. `VITE_APP_EXTENSIONS=example` appends it, which
 * is how the framework's own extension-point specs get an installed extension to
 * assert against, and how anyone can see it running without editing this file.
 */
export const activeAppExtensions: readonly AppExtensionConfig[] = [
  ...(import.meta.env.VITE_APP_EXTENSIONS === "example"
    ? [exampleAppExtension]
    : []),
];
