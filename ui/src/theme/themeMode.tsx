import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
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
 */
const STORAGE_KEY = "kagent.themeMode";

const ALL_MODES: readonly ThemeMode[] = ["dark", "light"];

interface ThemeModeContextValue {
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

const ThemeModeContext = createContext<ThemeModeContextValue | undefined>(undefined);

function storedMode(): ThemeMode | undefined {
  // Guarded: this module is imported by unit tests running without a DOM, and a
  // browser with storage disabled throws on access rather than returning null.
  try {
    const value = window.localStorage.getItem(STORAGE_KEY);
    return value === "dark" || value === "light" ? value : undefined;
  } catch {
    return undefined;
  }
}

function systemMode(): ThemeMode {
  return typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

export function ThemeModeProvider({
  children,
  supportedModes,
}: {
  children: ReactNode;
  /** Both, unless the installed extension can only be read in one. */
  supportedModes?: readonly ThemeMode[];
}) {
  const [chosen, setChosen] = useState<ThemeMode | undefined>(storedMode);
  const [system, setSystem] = useState<ThemeMode>(systemMode);

  // Followed for as long as the reader has not chosen: the subscription stays in
  // place either way, because a choice can be cleared and the system value has to
  // be current when it is.
  useEffect(() => {
    const query = window.matchMedia?.("(prefers-color-scheme: light)");
    if (!query) return;

    const onChange = (event: MediaQueryListEvent) =>
      setSystem(event.matches ? "light" : "dark");
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  const supported =
    supportedModes && supportedModes.length > 0 ? supportedModes : ALL_MODES;
  const preferred = chosen ?? system;
  // Clamped rather than trusted: a stored choice outlives the build that made it, so
  // a reader who picked light before an extension that cannot do light was installed
  // must not be left on an unreadable page.
  const mode = supported.includes(preferred) ? preferred : supported[0];

  // On the document element as well, for the things Emotion cannot reach: the
  // native scrollbars, form controls and selection colours the browser draws
  // itself, which follow `color-scheme` rather than any of our tokens.
  useEffect(() => {
    document.documentElement.dataset.theme = mode;
    document.documentElement.style.colorScheme = mode;
  }, [mode]);

  const setMode = useCallback((next: ThemeMode) => {
    setChosen(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // A reader with storage blocked still gets the theme for this session; the
      // alternative is refusing to change the theme at all, which is worse.
    }
  }, []);

  const value = useMemo<ThemeModeContextValue>(
    () => ({
      mode,
      isExplicit: chosen !== undefined,
      canToggle: supported.length > 1,
      setMode,
      toggle: () => setMode(mode === "dark" ? "light" : "dark"),
    }),
    [mode, chosen, setMode, supported.length],
  );

  return (
    <ThemeModeContext.Provider value={value}>{children}</ThemeModeContext.Provider>
  );
}

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
