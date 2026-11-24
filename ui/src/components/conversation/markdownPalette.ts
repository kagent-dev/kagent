import type { Theme } from "@emotion/react";
import type { ThemeMode } from "@/theme/theme";

const RGB_HEX = /^#[0-9a-f]{6}$/i;

/** `#rrggbb` or `#rrggbbaa`: the only colour forms Monaco's theme accepts. */
export const isHexColor = (value: string) => /^#[0-9a-f]{6}([0-9a-f]{2})?$/i.test(value);

/** `a` blended toward `b` by `amount` (0–1), as `#rrggbb`; `a` unchanged if either isn't `#rrggbb`. */
export function mix(a: string, b: string, amount: number): string {
  if (!RGB_HEX.test(a) || !RGB_HEX.test(b)) return a;
  const channels = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const [x, y] = [channels(a), channels(b)];
  return `#${x.map((v, i) => Math.round(v + (y[i] - v) * amount).toString(16).padStart(2, "0")).join("")}`;
}

/** Colours for rendered markdown and its code blocks, derived from the app theme. */
export interface MarkdownPalette {
  heading: string;
  body: string;
  muted: string;
  link: string;
  rule: string;
  quoteBg: string;
  quoteBar: string;
  quoteText: string;
  tableHeadBg: string;
  tableRule: string;
  codeBg: string;
  codeBorder: string;
  inlineCodeBg: string;
  syntax: SyntaxColors;
}

export interface SyntaxColors {
  keyword: string;
  key: string;
  string: string;
  number: string;
  comment: string;
  variable: string;
  punctuation: string;
}

/** Lavender: brand-tinted quotes and tables; dark mode lifts code off the black page. */
export function markdownPalette(theme: Theme, mode: ThemeMode): MarkdownPalette {
  const { color } = theme;
  const surface = color.bgElevated;
  if (mode === "dark") {
    return {
      heading: color.text,
      body: mix(color.text, surface, 0.12),
      muted: mix(mix(color.text, surface, 0.38), color.primaryText, 0.12),
      link: color.primaryText,
      rule: mix(surface, color.primaryText, 0.28),
      quoteBg: mix(surface, color.primaryText, 0.09),
      quoteBar: color.primaryText,
      quoteText: mix(color.text, surface, 0.16),
      tableHeadBg: mix(surface, color.primaryText, 0.07),
      tableRule: mix(surface, color.text, 0.12),
      codeBg: mix(surface, color.text, 0.045),
      codeBorder: mix(surface, color.text, 0.12),
      inlineCodeBg: mix(surface, color.text, 0.09),
      syntax: {
        keyword: "#c4b5fd",
        key: "#93c5fd",
        string: "#86efac",
        number: "#fcd34d",
        comment: "#9d98ab",
        variable: "#f0abfc",
        punctuation: "#b9b6c4",
      },
    };
  }
  return {
    heading: color.text,
    body: color.text,
    muted: color.textMuted,
    link: color.primaryText,
    rule: color.border,
    quoteBg: color.accentBg,
    quoteBar: color.primary,
    quoteText: mix(color.text, surface, 0.12),
    tableHeadBg: mix(surface, color.primary, 0.04),
    tableRule: color.border,
    codeBg: mix(surface, color.primary, 0.03),
    codeBorder: color.border,
    inlineCodeBg: mix(surface, color.primary, 0.06),
    syntax: {
      keyword: "#6d28d9",
      key: "#1d4ed8",
      string: "#15703a",
      number: "#a14a06",
      comment: "#6b6577",
      variable: "#9d1b8f",
      punctuation: "#57536a",
    },
  };
}
