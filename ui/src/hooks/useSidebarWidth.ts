"use client";

import { useCallback, useEffect, useState } from "react";

export const SIDEBAR_MIN_WIDTH = 200;
export const SIDEBAR_MAX_WIDTH = 480;

/**
 * Persistent, clamped sidebar width (px). Hydrates from localStorage after
 * mount (SSR-safe) and writes back on every change.
 */
export function useSidebarWidth(storageKey: string, defaultWidth: number) {
  const [width, setWidthState] = useState<number>(defaultWidth);

  useEffect(() => {
    try {
      const stored = Number(window.localStorage.getItem(storageKey));
      if (stored >= SIDEBAR_MIN_WIDTH && stored <= SIDEBAR_MAX_WIDTH) {
        // Post-mount hydration from localStorage; initializing state from
        // localStorage directly would mismatch the SSR-rendered width.
        // eslint-disable-next-line react-hooks/set-state-in-effect
        setWidthState(stored);
      }
    } catch {
      /* localStorage unavailable (private mode etc.) — keep default */
    }
  }, [storageKey]);

  const setWidth = useCallback(
    (next: number) => {
      const clamped = Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(next)));
      setWidthState(clamped);
      try {
        window.localStorage.setItem(storageKey, String(clamped));
      } catch {
        /* best-effort persistence */
      }
    },
    [storageKey]
  );

  const reset = useCallback(() => {
    setWidthState(defaultWidth);
    try {
      window.localStorage.removeItem(storageKey);
    } catch {
      /* best-effort */
    }
  }, [storageKey, defaultWidth]);

  return { width, setWidth, reset };
}
