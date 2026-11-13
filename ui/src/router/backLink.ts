import { useLocation, type Location } from "react-router-dom";

/**
 * Where a page was opened from, carried in router `state` rather than the URL.
 *
 * A detail page reached from somewhere other than its own list should lead back to
 * that place, not to the list. State rather than a query parameter, because the origin
 * is about this visit and not about the page: a copied or bookmarked URL opens the page
 * cold, and a cold visit has nowhere to go back to but the list.
 */
export interface BackLinkState {
  /** The path and search of the page to return to. */
  from: string;
  /** What the back control says, e.g. "Back to conversation". */
  fromLabel: string;
}

/** The `state` to pass a `Link` so the page it opens can lead back to the current one. */
export function backLinkState(
  location: Pick<Location, "pathname" | "search">,
  fromLabel: string,
): BackLinkState {
  return { from: location.pathname + location.search, fromLabel };
}

function isBackLinkState(value: unknown): value is BackLinkState {
  if (typeof value !== "object" || value === null) return false;
  const { from, fromLabel } = value as Record<string, unknown>;
  // Only an in-app path: state is set by our own links, but a path is what makes it
  // safe to navigate to whatever is found here.
  return (
    typeof from === "string" &&
    from.startsWith("/") &&
    !from.startsWith("//") &&
    typeof fromLabel === "string" &&
    fromLabel.length > 0
  );
}

/**
 * The page this one was opened from, or the fallback when it was visited directly.
 */
export function useBackLink(fallback: { to: string; label: string }): {
  to: string;
  label: string;
  /** True when `to` is the page this one was opened from, rather than the fallback. */
  isReturn: boolean;
} {
  const { state } = useLocation();
  return isBackLinkState(state)
    ? { to: state.from, label: state.fromLabel, isReturn: true }
    : { ...fallback, isReturn: false };
}
