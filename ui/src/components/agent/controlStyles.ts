import type { Theme } from "@emotion/react";

/**
 * Hover and pressed for a row or list item: the row tint, hover at half the press, instant.
 * `base` is a fill the row keeps under the tint, so a selected row stays distinct while hovered.
 */
export function rowStateStyles(theme: Theme, base?: string) {
  const over = (tint: string) => (base ? `linear-gradient(${tint}, ${tint}), ${base}` : tint);
  return {
    transition: "none",
    "&:hover": { background: over(theme.color.rowHoverBg) },
    "&:active": { background: over(theme.color.rowPressBg) },
  } as const;
}

/** An option card: the row tint plus a purple edge, both at half strength on hover. */
export function cardStateStyles(theme: Theme, surface?: string) {
  const { "&:hover": hover, "&:active": active } = rowStateStyles(theme, surface);
  return {
    transition: "none",
    "&:hover": { ...hover, borderColor: `${theme.color.primary}53` },
    "&:active": { ...active, borderColor: `${theme.color.primary}A6` },
  } as const;
}

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
  const selected = `${theme.color.primary}24`;
  const states = rowStateStyles(theme, isActive ? selected : undefined);
  return {
    display: "flex",
    alignItems: "center",
    gap: theme.space(2),
    padding: `${theme.space(2)} ${theme.space(3)}`,
    // The smaller of the two radii the rail uses, shared with the menu button beside
    // it: two rounded rectangles side by side with different corners read as a mistake.
    borderRadius: theme.radius.sm,
    minWidth: 0,
    color: isActive ? theme.color.text : theme.color.textMuted,
    /*
     * The open row is a tint and a weight, not an outlined pill.
     *
     * Outlined, it read as a selected field rather than the place you are — a hard
     * edge a few pixels from the menu button beside it, in a column of rows that have
     * none. Its purple differs from the greyed hover tint, and the heavier text carries
     * it when a row is scrolled past at speed.
     */
    background: isActive ? selected : "transparent",
    border: "1px solid transparent",
    fontWeight: isActive ? 600 : 400,
    // A translucent tint, so it shows inside the bgElevated switcher panel as well as on the page.
    transition: states.transition,
    "&:hover": { ...states["&:hover"], color: theme.color.text },
    "&:active": { ...states["&:active"], color: theme.color.text },
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
    transition: "none",
    "&:hover": {
      background: `${theme.color.text}14`,
      color: theme.color.text,
    },
    "&:active": {
      background: `${theme.color.text}29`,
      color: theme.color.text,
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
    transition: "none",
    "&:hover": { background: `${theme.color.text}14` },
    "&:focus-within": {
      background: theme.color.bg,
      borderColor: theme.color.primary,
    },
    "& input::placeholder": { color: theme.color.textMuted },
  } as const;
}

/**
 * A checkbox big enough to hit, and one that answers when you touch it.
 *
 * antd 6 draws the box on `.ant-checkbox` itself and the tick as its `::after` — there
 * is no `.ant-checkbox-inner` any more, which is what an override written against
 * antd 5 targets, and it silently styles nothing.
 *
 * The box stays antd's 16px, because the tick is positioned against that size; what
 * grows is the target around it. The padding does that and a negative margin of the
 * same size cancels it, so nothing beside the box moves.
 *
 * Hover rings the box in the brand purple at half strength and pressing takes it at
 * full, instantly. Both apply to the checked box too, which antd otherwise leaves inert.
 */
export function checkboxStyles(theme: Theme) {
  return {
    // antd's own 16px box, untouched: growing it moved the tick it positions against,
    // and a checkbox that changes size under the pointer is a target that moves.
    "& .ant-checkbox": {
      transition: "none",
      outline: "2px solid transparent",
      outlineOffset: 2,
    },
    "&:hover .ant-checkbox:not(.ant-checkbox-disabled)": {
      outline: `2px solid ${theme.color.primary}80`,
      outlineOffset: 2,
    },
    "&:active .ant-checkbox:not(.ant-checkbox-disabled)": {
      outline: `2px solid ${theme.color.primary}`,
      outlineOffset: 2,
    },
    // A keyboard reader has no hover to fall back on, and this is the only thing
    // telling them where they are.
    "&:focus-within .ant-checkbox": {
      outline: `2px solid ${theme.color.primaryText}`,
      outlineOffset: 2,
    },
  };
}

/**
 * The scrollbar the conversation uses, for the lists beside it.
 *
 * A thin thumb in the border colour that darkens when the box it is in is hovered.
 * Here rather than in each scrolling box, because two scrollbars a few hundred pixels
 * apart that do not match read as two applications.
 */
export function scrollbarStyles(theme: Theme) {
  return {
    scrollbarWidth: "thin" as const,
    scrollbarColor: `${theme.color.border} transparent`,
    "&::-webkit-scrollbar": { width: 10 },
    "&::-webkit-scrollbar-track": { background: "transparent" },
    "&::-webkit-scrollbar-thumb": {
      background: theme.color.border,
      borderRadius: 999,
      border: "3px solid transparent",
      backgroundClip: "content-box" as const,
    },
    "&:hover::-webkit-scrollbar-thumb": { background: theme.color.textMuted },
  };
}
