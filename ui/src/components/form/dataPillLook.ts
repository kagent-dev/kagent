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

/** Hover and pressed fills for a pill's remove control. */
export function dataPillRemoveStates(theme: Theme) {
  return {
    "&:hover": { background: `${theme.color.text}29`, color: theme.color.text },
    "&:active": { background: `${theme.color.text}3d`, color: theme.color.text },
  };
}
