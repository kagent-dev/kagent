import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { THEME_MODE_STORAGE_KEY } from "./storedMode";
import { ThemeModeProvider } from "./themeMode";
import { useThemeMode } from "./useThemeMode";

describe("theme mode context", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.spyOn(window, "matchMedia").mockReturnValue({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    } as unknown as MediaQueryList);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    window.localStorage.clear();
    delete document.documentElement.dataset.theme;
    document.documentElement.style.colorScheme = "";
  });

  it("keeps the dark, non-toggleable fallback outside a provider", () => {
    const { result } = renderHook(useThemeMode);
    expect(result.current).toMatchObject({ mode: "dark", isExplicit: false, canToggle: false });
    act(() => result.current.toggle());
    expect(result.current.mode).toBe("dark");
  });

  it("reads the provider's system mode and persists an explicit toggle", () => {
    const { result } = renderHook(useThemeMode, { wrapper: ThemeModeProvider });
    expect(result.current).toMatchObject({ mode: "light", isExplicit: false, canToggle: true });
    expect(window.localStorage.getItem(THEME_MODE_STORAGE_KEY)).toBeNull();

    act(() => result.current.toggle());
    expect(result.current).toMatchObject({ mode: "dark", isExplicit: true });
    expect(window.localStorage.getItem(THEME_MODE_STORAGE_KEY)).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("shares a stored preference between the provider and hook modules", () => {
    window.localStorage.setItem(THEME_MODE_STORAGE_KEY, "dark");
    const { result } = renderHook(useThemeMode, { wrapper: ThemeModeProvider });
    expect(result.current).toMatchObject({ mode: "dark", isExplicit: true, canToggle: true });
  });

  it("clamps the stored preference to an extension's supported palette", () => {
    window.localStorage.setItem(THEME_MODE_STORAGE_KEY, "light");
    const { result } = renderHook(useThemeMode, {
      wrapper: ({ children }) => (
        <ThemeModeProvider supportedModes={["dark"]}>{children}</ThemeModeProvider>
      ),
    });
    expect(result.current).toMatchObject({ mode: "dark", isExplicit: true, canToggle: false });
  });
});
