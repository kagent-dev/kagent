import type { Theme } from "@emotion/react";

/**
 * How a bare router link is inked, since antd's `colorLink` does not reach one.
 *
 * Without this a table's own text colour wins and the only navigable cell on a row
 * reads as plain text. Pressed goes to the page foreground rather than `primaryHover`,
 * a darker purple that loses contrast on the dark theme.
 */
export function linkInk(theme: Theme) {
  return {
    color: theme.color.primaryText,
    "&:hover": { color: theme.color.primaryText, textDecoration: "underline" },
    "&:active": { color: theme.color.text, textDecoration: "underline" },
  };
}

/** The same, for a container that holds several links. */
export function linkStyles(theme: Theme) {
  return { a: linkInk(theme) };
}
