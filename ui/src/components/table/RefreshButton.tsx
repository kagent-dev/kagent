import { useState } from "react";
import { Button } from "antd";
import { RotateCw } from "lucide-react";
import toast from "react-hot-toast";

/**
 * A list's Refresh control, which says what happened.
 *
 * Pressing refresh used to be the one action in the app with no acknowledgement:
 * the data is usually the same, so a successful refresh looked identical to a
 * button that did nothing. A line of confirmation is the whole point of this
 * component.
 *
 * It is careful about which line. `refresh` rejects when the refetch failed, so a
 * failure says so rather than reporting a refresh that did not happen — a toast
 * claiming "Agents refreshed" above a page showing a load error is worse than
 * silence, because the two contradict each other and the reader has to work out
 * which to believe.
 *
 * One component rather than seven copies: every list needs the same behaviour, and
 * the next one added should not have to remember to toast.
 */
/**
 * The refresh-and-confirm behaviour, without a particular button around it.
 *
 * Separate from the button because not every refresh control is one: a product
 * skin may draw an icon in its own style, and it should not have to restate any of
 * this — least of all the part that decides *which* message to show, which is the
 * part that was wrong the first time.
 */
export function useRefreshToast(
  onRefresh: () => Promise<unknown>,
  what: string,
): { refresh: () => void; refreshing: boolean } {
  const [refreshing, setRefreshing] = useState(false);

  const run = async () => {
    // Guarded rather than relying on a disabled state: a second click while the
    // first is in flight would produce two toasts for one refresh.
    if (refreshing) return;

    setRefreshing(true);
    try {
      await onRefresh();
      toast.success(`${what} refreshed`);
    } catch (cause) {
      // The reason where there is one: a refresh that failed because nothing is
      // listening and one that failed on a rejected credential need different
      // things done about them.
      const detail = cause instanceof Error ? cause.message : undefined;
      toast.error(
        detail ? `Could not refresh ${what}: ${detail}` : `Could not refresh ${what}`,
      );
    } finally {
      setRefreshing(false);
    }
  };

  return { refresh: () => void run(), refreshing };
}

export function RefreshButton({
  onRefresh,
  what,
  loading = false,
  disabled = false,
  "data-testid": testId = "refresh-button",
}: {
  /** Refetches, and rejects if the refetch failed. */
  onRefresh: () => Promise<unknown>;
  /** What was refreshed, as it should read in the message: "Agents", "Models". */
  what: string;
  /** Whether a request is already in flight from somewhere else. */
  loading?: boolean;
  /** For a page with nothing to refetch — a resource that is genuinely absent. */
  disabled?: boolean;
  "data-testid"?: string;
}) {
  const { refresh, refreshing } = useRefreshToast(onRefresh, what);

  return (
    <Button
      onClick={refresh}
      loading={loading || refreshing}
      disabled={disabled}
      data-testid={testId}
      // An icon at rest, so the spinner has somewhere to go. Without one the library
      // inserts the spinner *beside* the label and animates the button wider to fit it,
      // so every refresh made the control grow and shrink — and the buttons next to it
      // shuffle. With an icon present the spinner replaces it in the same slot.
      icon={<RotateCw size={14} aria-hidden />}
    >
      Refresh
    </Button>
  );
}
