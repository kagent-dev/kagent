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

/**
 * A link inside running text: brand colour at rest, underlined on hover, and the page
 * foreground when pressed. Decoration and colour only, so no state moves the text.
 */
export function inlineLink(theme: Theme) {
  const underline = { textDecorationLine: "underline", textDecorationThickness: 1 };
  return {
    color: theme.color.primaryText,
    // Medium weight marks a link at rest without an underline; it never changes, so nothing shifts.
    fontWeight: 500,
    textDecoration: "none",
    textUnderlineOffset: 2,
    borderRadius: 3,
    transition: "none",
    "&:hover": { color: theme.color.primaryText, ...underline },
    "&:active": { color: theme.color.text, ...underline },
    "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 2 },
  };
}

/** The same, for a container that holds several links. */
export function linkStyles(theme: Theme) {
  return { a: linkInk(theme) };
}
