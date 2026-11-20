import type { Theme } from "@emotion/react";

/**
 * The data pill's look, shared by `DataPill` and tag-mode selects in GlobalStyles.
 * Square corners and mono type keep it apart from buttons and status tags.
 */
export function dataPillLook(theme: Theme) {
  return {
    background: `${theme.color.text}14`,
    border: `1px solid ${theme.color.borderStrong}`,
    borderRadius: 4,
    fontFamily: theme.font.mono,
    fontSize: 12,
    color: theme.color.text,
  };
}

/** Hover, pressed and focus looks for a pill that removes itself when clicked. */
export function dataPillButtonStates(theme: Theme) {
  return {
    cursor: "pointer",
    "&:hover:not(:disabled)": { background: `${theme.color.text}29`, borderColor: theme.color.text },
    "&:active:not(:disabled)": { background: `${theme.color.text}3d`, borderColor: theme.color.text },
    "&:hover:not(:disabled) [data-pill-x], &:active:not(:disabled) [data-pill-x]": {
      background: theme.color.dangerBg,
      color: theme.color.dangerText,
    },
    "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 1 },
    "&:disabled": { cursor: "not-allowed", opacity: 0.6 },
  };
}
