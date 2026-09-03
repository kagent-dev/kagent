import { createContext } from "react";
import type { AppExtensionConfig } from "./types";

/**
 * The empty install.
 *
 * A module-level constant rather than a fresh `[]` at each default, so the identity
 * is stable and the `useMemo`s that fold over it do not recompute every render.
 */
export const NO_APP_EXTENSIONS: readonly AppExtensionConfig[] = [];

/**
 * Carries the installed extensions, in order.
 *
 * Defaulted to the empty install rather than `undefined` so a component rendered
 * outside the provider — a unit test, a Storybook story — behaves exactly like the
 * no-extension case instead of throwing.
 */
export const AppExtensionContext =
  createContext<readonly AppExtensionConfig[]>(NO_APP_EXTENSIONS);
