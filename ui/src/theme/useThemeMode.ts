import { createContext, useContext } from "react";
import type { ThemeMode } from "./theme";

export interface ThemeModeContextValue {
  mode: ThemeMode;
  /** Whether the mode is the reader's own choice rather than the system's. */
  isExplicit: boolean;
  /**
   * Whether there is more than one palette to switch between.
   *
   * False when the installed extension supports only one. The control that toggles
   * should not be drawn at all in that case — a toggle that cannot change anything
   * is worse than its absence, because pressing it looks like a bug.
   */
  canToggle: boolean;
  setMode: (mode: ThemeMode) => void;
  toggle: () => void;
}

export const ThemeModeContext = createContext<ThemeModeContextValue | undefined>(undefined);

/**
 * The current mode and the toggle.
 *
 * Falls back to dark outside a provider rather than throwing: this is read by
 * chrome that a test may mount on its own, and a missing provider should cost a
 * default palette rather than a blank page.
 */
export function useThemeMode(): ThemeModeContextValue {
  return (
    useContext(ThemeModeContext) ?? {
      mode: "dark",
      isExplicit: false,
      canToggle: false,
      setMode: () => {},
      toggle: () => {},
    }
  );
}
