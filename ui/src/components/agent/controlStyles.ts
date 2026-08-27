import type { Theme } from "@emotion/react";

/**
 * How a row in the agent navigation reacts to a pointer.
 *
 * Shared because three lists use it — the rail's conversations, the switcher's agents,
 * and anything added beside them — and a list that highlights differently from the list
 * above it reads as two different controls rather than one idiom.
 *
 * Every colour comes from a token, so the same declaration darkens on the light theme
 * and lightens on the dark one.
 */
export function rowStyles(theme: Theme, isActive: boolean) {
  return {
    display: "flex",
    alignItems: "center",
    gap: theme.space(2),
    padding: `${theme.space(2)} ${theme.space(3)}`,
    borderRadius: theme.radius.md,
    minWidth: 0,
    color: isActive ? theme.color.text : theme.color.textMuted,
    background: isActive ? `${theme.color.primary}26` : "transparent",
    border: `1px solid ${isActive ? theme.color.primary : "transparent"}`,
    transition: "background 100ms ease, color 100ms ease",
    /*
     * A tint of the foreground, not a named surface.
     *
     * Hover used to be `bgElevated`, which works on the page but vanishes inside the
     * agent switcher — that panel *is* `bgElevated`, so hovering a row there changed it
     * to the colour it was already sitting on and the rows looked inert. A tint darkens
     * on the light theme and lightens on the dark one from one declaration, and shows
     * against either ground.
     */
    "&:hover": { background: `${theme.color.text}14`, color: theme.color.text },
    /*
     * Pressed, deeper than hover and instant.
     *
     * A row had hover and nothing for the click, so on a slow route the press did not
     * register visually and the reader clicked again. A tint of the brand for the row
     * you are on — which must not appear to lose its highlight while being pressed —
     * and the border colour for the rest.
     */
    "&:active": {
      background: isActive ? `${theme.color.primary}40` : `${theme.color.text}29`,
      color: theme.color.text,
      transition: "none",
    },
  } as const;
}

/**
 * How the small icon controls around the conversation react to a pointer.
 *
 * The sidebar toggles and the share button are `type="text"`, which antd gives a very
 * faint hover — on a page that is mostly text they read as decoration, and a reader
 * cannot tell they are clickable until they have already clicked. These are the only
 * controls at the top of a conversation, so they have to look like controls.
 *
 * Same idiom as `rowStyles`: a tint on hover, a deeper one on press, instant so the
 * click registers even when what it triggers is slow.
 */
export function iconControlStyles(theme: Theme) {
  return {
    color: theme.color.textMuted,
    transition: "background 100ms ease, color 100ms ease",
    "&:hover": {
      background: `${theme.color.text}14`,
      color: theme.color.text,
    },
    "&:active": {
      background: `${theme.color.text}29`,
      color: theme.color.text,
      transition: "none",
    },
  } as const;
}

/**
 * How a search box inside the agent navigation looks.
 *
 * Shared by the rail's conversation search and the switcher's agent search: two inputs
 * a few pixels apart that looked different read as two unrelated controls. The default
 * input carries a hard border and the page's own background, which in a column of
 * tinted rows is the one element that looks dropped in from a settings page — so it
 * takes the rows' ground and radius, and finds its border only on focus.
 */
export function searchInputStyles(theme: Theme) {
  return {
    background: `${theme.color.text}0f`,
    border: "1px solid transparent",
    borderRadius: theme.radius.md,
    paddingBlock: theme.space(1),
    paddingInline: theme.space(2),
    transition: "background 100ms ease, border-color 100ms ease",
    "&:hover": { background: `${theme.color.text}14` },
    "&:focus-within": {
      background: theme.color.bg,
      borderColor: theme.color.primary,
    },
    "& input::placeholder": { color: theme.color.textMuted },
  } as const;
}
