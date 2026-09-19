import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { ThemeMode } from "./theme";
import { ThemeModeContext, type ThemeModeContextValue } from "./useThemeMode";
import { THEME_MODE_STORAGE_KEY, storedMode } from "./storedMode";

const ALL_MODES: readonly ThemeMode[] = ["dark", "light"];

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
      window.localStorage.setItem(THEME_MODE_STORAGE_KEY, next);
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
