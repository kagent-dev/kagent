import type { ThemeConfig } from "antd";

/**
 * Design tokens, in one place so a vendor extension can override the look
 * without reaching into components. antd consumes `antdTheme`; Emotion
 * consumes `appTheme` via the ThemeProvider.
 */
/** Which of the two palettes is in force. */
export type ThemeMode = "dark" | "light";

/**
 * The two palettes.
 *
 * Both are purple-leaning rather than blue. The borders used to be `#1f2937` — a
 * slate gray whose blue reads clearly against a near-black page, so every card,
 * header and table looked outlined in navy. These are a mid gray tinted toward the
 * brand purple instead, which sits with the surfaces rather than ringing them.
 *
 * `surfaceCard` and `surfaceNav` are gradients, not flat colours: a very shallow
 * one (three or four steps of lightness) gives a panel some depth without becoming
 * a decoration in its own right. They live beside the colours because that is where
 * components look for a surface, and any of these may be a plain colour again if a
 * vendor overrides it.
 */
const darkColor = {
  primary: "#6d28d9",
  primaryHover: "#5b21b6",
  /**
   * Text on a `primary` surface.
   *
   * Its own token because `text` is the foreground for the *page*, and on this theme
   * the two happen to agree — which is what hid the bug. On the light theme `text` is
   * near-black, and anything painted on the primary purple with it came out as dark on
   * dark: the chat's own messages were the worst of it. A vendor overriding `primary`
   * needs somewhere to say what goes on top of it, too.
   */
  textOnPrimary: "#f9fafb",
  bg: "#08070c",
  bgElevated: "#17151d",
  surfaceCard: "linear-gradient(180deg, #1b1824 0%, #16141d 100%)",
  surfaceNav: "linear-gradient(180deg, #1a1722 0%, #141219 100%)",
  border: "#322c3d",
  text: "#f9fafb",
  textMuted: "#9ca3af",
  success: "#22c55e",
  warning: "#f59e0b",
  // Not the value the previous design system carried for "destructive". That
  // was a dark surface meant to sit behind light text, whereas this token is
  // the foreground: it colours error text, borders and icons. Using the
  // surface value here would make a failure message almost invisible against
  // the page. A mid red is the same intent expressed for a foreground.
  danger: "#ef4444",
  /**
   * Backgrounds, borders and text for a status pill, per status.
   *
   * Stated rather than derived. antd builds a tag's three colours from the one
   * foreground token above, and its derivation assumes a light page: on this theme
   * it produced a mid-green fill under near-white text — legible, but reading as a
   * filled badge rather than the quiet pill the rest of the page uses.
   */
  successBg: "#0c2c18",
  successBorder: "#166534",
  successText: "#4ade80",
  warningBg: "#33240a",
  warningBorder: "#92400e",
  warningText: "#fbbf24",
  dangerBg: "#3a1417",
  dangerBorder: "#991b1b",
  dangerText: "#f87171",
  /**
   * The brand colour as *foreground* text on the page.
   *
   * `primary` is a deep purple chosen as a surface with light text on it, and read against
   * this theme's near-black page it measures 2.5:1 — the prompt library's names were the
   * only navigable text on their row and the hardest thing on the page to read. A
   * foreground needs its own value for the same reason `textOnPrimary` does: one token
   * cannot be both the fill and the ink.
   */
  primaryText: "#a78bfa",
  /**
   * Two more pill triples, for cells that classify rather than report health.
   *
   * Which sort of tool server a row is, for instance: borrowing success or warning for
   * that would say something untrue about it. antd's presets fill the gap with colours
   * derived for a light page — on this theme its `geekblue` and `purple` tags measured
   * 4.2:1 and 3.4:1, both under the 4.5 that small text needs.
   */
  infoBg: "#101c33",
  infoBorder: "#1e3a8a",
  infoText: "#93c5fd",
  accentBg: "#1e152e",
  accentBorder: "#5b21b6",
  accentText: "#c4b5fd",
  /**
   * The border of something you can act on — an input, a select, a picker.
   *
   * `border` is for separating surfaces, and at 1.35:1 against this page it is right for
   * a card's edge and too quiet for a control: a search field read as a faint smudge
   * rather than as a box to type in. Non-text contrast wants 3:1 for a component
   * boundary, which this clears.
   */
  borderStrong: "#6f6a83",
} as const;

const lightColor: Record<keyof typeof darkColor, string> = {
  primary: "#6d28d9",
  primaryHover: "#5b21b6",
  // The same white: `primary` is a deep purple on both themes, so what reads on it
  // does not change when the page around it does.
  textOnPrimary: "#f9fafb",
  bg: "#f6f5f9",
  bgElevated: "#ffffff",
  surfaceCard: "linear-gradient(180deg, #ffffff 0%, #faf9fd 100%)",
  surfaceNav: "linear-gradient(180deg, #fbfafd 0%, #f2f0f7 100%)",
  border: "#ddd7e7",
  // The wordmark's own dark, so type and logo agree rather than nearly agreeing.
  text: "#151927",
  textMuted: "#6b6577",
  success: "#15803d",
  warning: "#b45309",
  // Darker than the dark theme's red for the same reason that one is not the old
  // surface value: on white, `#ef4444` is a pale warning rather than an error.
  danger: "#dc2626",
  // Pale fill, saturated border, dark text — the same shape as the dark theme's,
  // inverted. What was there before came out muddy: antd tinted its mid-green
  // towards a light base and landed on something between the two.
  successBg: "#dcfce7",
  successBorder: "#86efac",
  successText: "#166534",
  warningBg: "#fef3c7",
  warningBorder: "#fcd34d",
  warningText: "#92400e",
  dangerBg: "#fee2e2",
  dangerBorder: "#fca5a5",
  dangerText: "#991b1b",
  // On a white page the brand purple is already a readable foreground, so this is
  // `primary`. The token exists so a component need not know which theme it is on.
  primaryText: "#6d28d9",
  infoBg: "#eff6ff",
  infoBorder: "#bfdbfe",
  infoText: "#1d4ed8",
  accentBg: "#f5f3ff",
  accentBorder: "#ddd6fe",
  accentText: "#6d28d9",
  /*
   * Same intent on a white page, where `border` measured 1.4:1.
   *
   * Darkened once since: at `#9490a6` it cleared 3:1 on white by four hundredths and
   * fell to 2.85:1 on any surface a shade off it — a panel, a card, a product skin's
   * own background — which is most of the places an input is actually drawn. This
   * clears it on both with room to spare (3.5:1 on white, 3.2:1 on a near-white panel).
   */
  borderStrong: "#8a86a0",
};

export const tokens = {
  color: darkColor,
  // Derived the way the previous design system derived them, from a 12px base:
  // the largest is the base, and the smaller two step down from it. The old
  // 4/8/12 scale made every corner tighter than the app it replaced.
  radius: { sm: 8, md: 10, lg: 12 },
  space: (n: number) => `${n * 4}px`,
  font: {
    body: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    mono: 'ui-monospace, SFMono-Regular, Menlo, monospace',
  },
  layout: { headerHeight: 56, sidebarWidth: 240, sidebarCollapsedWidth: 68 },
} as const;

export interface AppTheme {
  color: Record<keyof typeof tokens.color, string>;
  radius: Record<keyof typeof tokens.radius, number>;
  space: (n: number) => string;
  font: Record<keyof typeof tokens.font, string>;
  layout: Record<keyof typeof tokens.layout, number>;
}

/** The tokens for one mode. `appTheme` is the dark set, which is the default. */
export function themeFor(mode: ThemeMode): AppTheme {
  return { ...tokens, color: mode === "light" ? lightColor : darkColor };
}

export const appTheme: AppTheme = themeFor("dark");

/**
 * The component library's config for one mode.
 *
 * Surfaces are pinned rather than derived. antd tints every surface from
 * `colorBgBase`, and on a near-black page that derivation is what turned tables
 * and cards visibly navy — the design this follows had no such tint. Naming the
 * surface keeps them in the same family as the page.
 *
 * `colorBgContainer` gets the flat elevated colour and not `surfaceCard`: antd
 * puts this value straight into `background-color`, which cannot take a gradient.
 * Components that want the gradient reach for the token themselves.
 */
export function antdThemeFor(mode: ThemeMode): ThemeConfig {
  const color = mode === "light" ? lightColor : darkColor;

  return {
    token: {
      colorPrimary: color.primary,
      colorBgBase: color.bg,
      colorTextBase: color.text,
      colorBorder: color.border,
      colorSuccess: color.success,
      colorWarning: color.warning,
      colorError: color.danger,
      // The three-part surfaces antd would otherwise derive. Tag, Alert and Badge
      // all read these, so a status looks the same wherever it is shown.
      colorSuccessBg: color.successBg,
      colorSuccessBorder: color.successBorder,
      colorSuccessText: color.successText,
      colorWarningBg: color.warningBg,
      colorWarningBorder: color.warningBorder,
      colorWarningText: color.warningText,
      colorErrorBg: color.dangerBg,
      colorErrorBorder: color.dangerBorder,
      colorErrorText: color.dangerText,
      // Anything the library draws as a link takes the readable purple, not the fill.
      colorLink: color.primaryText,
      colorLinkHover: color.primaryText,
      borderRadius: tokens.radius.md,
      fontFamily: tokens.font.body,
      colorBgContainer: color.bgElevated,
      colorBgElevated: color.bgElevated,
    },
    components: {
      Layout: {
        headerBg: color.bgElevated,
        bodyBg: color.bg,
        siderBg: color.bgElevated,
      },
      Table: {
        headerBg: color.bgElevated,
        rowHoverBg: color.border,
      },
      /*
       * Inputs, selects and pickers: a stronger edge and a legible placeholder.
       *
       * antd derives both from the shared border and text tokens, which are tuned for
       * surfaces and prose. Measured before this: a control border at 1.35:1 on the dark
       * theme and 1.4:1 on the light one, and placeholder text at a quarter opacity —
       * about 2.2:1. The border is a component boundary (3:1) and the placeholder is text
       * somebody has to read to know what the field is for.
       */
      Input: {
        colorBorder: color.borderStrong,
        colorTextPlaceholder: color.textMuted,
      },
      Select: {
        colorBorder: color.borderStrong,
        colorTextPlaceholder: color.textMuted,
        /*
         * The chevron, which antd paints from its quaternary text token — a quarter
         * opacity, measured at 2.2:1. It is the only mark saying the control opens, so it
         * was the least visible part of the thing a reader is looking for.
         */
        colorTextQuaternary: color.textMuted,
      },
      DatePicker: {
        colorBorder: color.borderStrong,
        colorTextPlaceholder: color.textMuted,
      },
      InputNumber: {
        colorBorder: color.borderStrong,
        colorTextPlaceholder: color.textMuted,
      },
      Radio: {
        /*
         * The selected option's label and border are the brand colour used as ink, and
         * antd takes that from `colorPrimary` — which is the fill. On the dark theme the
         * agent type toggle's chosen half measured 2.2:1 against the page while the
         * unchosen half sat at 13:1, so the option that was *not* selected was the one you
         * could read. Pointed at `primaryText` for the same reason the prompt links are.
         */
        colorPrimary: color.primaryText,
        colorPrimaryHover: color.primaryText,
      },
      /*
       * The selected tab, in ink rather than in fill.
       *
       * antd colours a selected tab and its ink bar from `colorPrimary`, which is the
       * deep purple chosen as a *background* with light text on it. On the dark theme
       * that measured 2.37:1 against the page while the unselected tabs sat at 19:1 —
       * so the tab you were not on was the one you could read, which is the same
       * mistake recorded against the segmented control above and the prompt links.
       */
      Tabs: {
        itemSelectedColor: color.primaryText,
        itemHoverColor: color.primaryText,
        inkBarColor: color.primaryText,
      },
      Menu: {
        darkItemBg: "transparent",
        darkSubMenuItemBg: "transparent",
        itemBg: "transparent",
        subMenuItemBg: "transparent",
        // Selected and hover, stated for both themes.
        //
        // Left to antd, the dark menu paints the selected row in solid primary — a
        // filled purple band, which is the opposite of the inset pill the rail is
        // shaped for, and it left the pressed state indistinguishable from the
        // settled one. A tint of the primary over the rail reads as a pill and lets
        // hover, press and selected be three visible steps.
        darkItemSelectedBg: "rgba(139, 92, 246, 0.22)",
        darkItemSelectedColor: "#f9fafb",
        darkItemHoverBg: "rgba(249, 250, 251, 0.07)",
        darkItemHoverColor: "#f9fafb",
        itemSelectedBg: "rgba(109, 40, 217, 0.1)",
        itemSelectedColor: color.primaryText,
        itemHoverBg: "rgba(21, 25, 39, 0.04)",
      },
    },
  };
}

export const antdTheme: ThemeConfig = antdThemeFor("dark");

declare module "@emotion/react" {
  // Emotion's Theme is intentionally empty; this widens it to our tokens so
  // styled components get autocomplete on `props.theme`. Declaration merging
  // needs an interface here, so the empty body is load-bearing.
  // eslint-disable-next-line @typescript-eslint/no-empty-object-type
  export interface Theme extends AppTheme {}
}
