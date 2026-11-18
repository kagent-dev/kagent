import type { AppTheme } from "@/theme/theme";

/** One pill triple from the theme: fill, edge and ink. */
export interface AuthorTone {
  background: string;
  border: string;
  text: string;
}

/**
 * The colour a workflow node's replies are labelled with.
 *
 * A workflow answers through several agents — a writer, its critics, the loop that
 * runs them — and each reply arrives as its own message naming the node. Like
 * adk-web, the label's colour is a hash of that name, so one node keeps one colour
 * across the transcript and across reloads. The pool is the theme's classifying
 * pill triples, which already clear text contrast on both themes; danger is left
 * out so no node reads as an error.
 */
export function authorTone(author: string, theme: AppTheme): AuthorTone {
  const { color } = theme;
  const tones: AuthorTone[] = [
    { background: color.infoBg, border: color.infoBorder, text: color.infoText },
    { background: color.accentBg, border: color.accentBorder, text: color.accentText },
    { background: color.successBg, border: color.successBorder, text: color.successText },
    { background: color.warningBg, border: color.warningBorder, text: color.warningText },
  ];
  let hash = 0;
  for (const char of author) {
    hash = (hash * 31 + char.codePointAt(0)!) >>> 0;
  }
  return tones[hash % tones.length];
}
