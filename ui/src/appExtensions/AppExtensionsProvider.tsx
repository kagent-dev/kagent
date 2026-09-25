import { useMemo } from "react";
import type { ReactNode } from "react";
import { AppExtensionContext } from "./context";
import { validateAppExtensions } from "./validateConfig";
import type { AppExtensionConfig } from "./types";

interface AppExtensionsProviderProps {
  /** The installed extensions, in order. Earlier entries are applied first. */
  extensions: readonly AppExtensionConfig[];
  /** Core route paths, so a contributed route colliding with one is caught. */
  reservedPaths?: readonly string[];
  children: ReactNode;
}

/**
 * Publishes the installed app extensions to the tree.
 *
 * Validation runs here, during the first render, so a bad install fails at boot
 * with a list of problems rather than as a component that quietly never appears.
 * It checks the whole array rather than each config alone, because two extensions
 * can be individually valid and still collide — over a route path, a nav key, or an
 * id — and only the install as a whole can see that.
 */
export function AppExtensionsProvider({
  extensions,
  reservedPaths,
  children,
}: AppExtensionsProviderProps) {
  useMemo(
    () => validateAppExtensions(extensions, reservedPaths),
    [extensions, reservedPaths],
  );

  return (
    <AppExtensionContext.Provider value={extensions}>
      {children}
    </AppExtensionContext.Provider>
  );
}
