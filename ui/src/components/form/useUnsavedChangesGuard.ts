import { useEffect } from "react";
import { useBlocker, type Blocker } from "react-router-dom";

/**
 * Asks before leaving a dirty form: route changes are blocked for the caller's
 * modal, and reloads or closed tabs get the browser's own prompt.
 */
export function useUnsavedChangesGuard(isDirty: boolean): Blocker {
  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) => isDirty && currentLocation.pathname !== nextLocation.pathname,
  );

  useEffect(() => {
    if (!isDirty) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [isDirty]);

  return blocker;
}
