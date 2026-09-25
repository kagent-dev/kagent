import type { ThemeMode } from "./theme";

/**
 * Which palette the reader has chosen, remembered between visits.
 *
 * Three states rather than two, and the distinction is the whole design: "dark",
 * "light", or *unset*. Unset follows the operating system, so a reader who has
 * never touched the toggle gets the theme the rest of their machine is using and
 * keeps getting it when they change that. Storing a resolved value on first load
 * would silently pin them to whatever they happened to be using that day.
 *
 * Only an explicit choice is written down, which is also what makes the toggle
 * honest: it does not appear to do nothing when the system disagrees with it.
 *
 * Its own module so the root error boundary can read the mode without importing
 * the provider it renders above.
 */
export const THEME_MODE_STORAGE_KEY = "kagent.themeMode";

export function storedMode(): ThemeMode | undefined {
  // Guarded: this is imported by unit tests running without a DOM, and a browser
  // with storage disabled throws on access rather than returning null.
  try {
    const value = window.localStorage.getItem(THEME_MODE_STORAGE_KEY);
    return value === "dark" || value === "light" ? value : undefined;
  } catch {
    return undefined;
  }
}
