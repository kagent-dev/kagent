import { useState } from "react";
import { Button, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";

const { Text } = Typography;

/**
 * A paged section's position, as a stack of the tokens that got us here.
 *
 * A stack rather than a page number, because the API pages by token: the only way
 * back to the previous page is the token that produced it. Reset whenever the
 * question changes — a new filter or a new scope makes every token meaningless,
 * and reusing one would ask for "the page after a row that is no longer in the
 * result".
 */
export function usePageStack(resetKey: string) {
  const [state, setState] = useState<{ key: string; tokens: string[] }>({
    key: resetKey,
    tokens: [],
  });

  /*
   * The reset is derived, not performed.
   *
   * Clearing the stack from an effect would be a `setState` inside one — a cascading
   * render, and the rule that forbids it is right — and it would also render one
   * frame of the *old* page against the new question before correcting itself.
   * Reading the key alongside the tokens means a changed question is already on the
   * first page in the render that discovers it.
   */
  const tokens = state.key === resetKey ? state.tokens : [];

  return {
    /** The token for the page being shown. Empty is the first page. */
    current: tokens.length > 0 ? tokens[tokens.length - 1] : "",
    pageNumber: tokens.length + 1,
    canGoBack: tokens.length > 0,
    next: (token: string) =>
      setState({ key: resetKey, tokens: [...tokens, token] }),
    back: () => setState({ key: resetKey, tokens: tokens.slice(0, -1) }),
    /** Back to the first page, for a change that reorders the result under us. */
    reset: () => setState({ key: resetKey, tokens: [] }),
  };
}

/** Previous and Next for one paged section, with the page number between them. */
export function PageControls({
  testId,
  page,
  hasNext,
  onNext,
  onBack,
  isLoading,
}: {
  testId: string;
  page: { pageNumber: number; canGoBack: boolean };
  hasNext: boolean;
  onNext: () => void;
  onBack: () => void;
  /*
   * First load only, never a background revalidation: a table that refreshes on a
   * timer would otherwise take its own pagination away on every poll.
   */
  isLoading: boolean;
}) {
  const theme = useTheme();

  // Nothing to page through: one page and no way off it. Hidden rather than shown
  // disabled, because two dead buttons under a five-row table read as a broken
  // control rather than as a complete list.
  if (!hasNext && !page.canGoBack) return null;

  return (
    <Space size={8} css={{ marginTop: theme.space(3) }} data-testid={testId}>
      <Button
        size="small"
        disabled={!page.canGoBack || isLoading}
        onClick={onBack}
        data-testid={`${testId}-prev`}
      >
        Previous
      </Button>
      <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
        Page {page.pageNumber}
      </Text>
      <Button
        size="small"
        disabled={!hasNext || isLoading}
        onClick={onNext}
        data-testid={`${testId}-next`}
      >
        Next
      </Button>
    </Space>
  );
}
