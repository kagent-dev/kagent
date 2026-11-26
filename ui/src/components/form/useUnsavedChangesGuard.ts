import { useCallback, useEffect, useRef } from "react";
import { useBlocker, type Blocker } from "react-router-dom";

/**
 * Asks before leaving a dirty form: route changes are blocked for the caller's
 * modal, and reloads or closed tabs get the browser's own prompt.
 * `release()` turns the guard off at once, so a save can navigate straight after.
 */
export function useUnsavedChangesGuard(isDirty: boolean): { blocker: Blocker; release: () => void } {
  const released = useRef(false);
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      isDirty && !released.current && currentLocation.pathname !== nextLocation.pathname,
  );

  useEffect(() => {
    if (!isDirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      if (!released.current) event.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [isDirty]);

  const release = useCallback(() => {
    released.current = true;
  }, []);

  return { blocker, release };
}
