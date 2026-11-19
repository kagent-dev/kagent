import { lazy, Suspense, useMemo } from "react";
import { css, useTheme } from "@emotion/react";
import type { Theme } from "@emotion/react";
import { useThemeMode } from "@/theme/useThemeMode";
import { resolveCodeLanguage } from "./codeLanguage";
import { markdownPalette, type MarkdownPalette } from "./markdownPalette";
import { CODE_METRICS } from "./codeMetrics";

// Monaco is large: the first code block on the page pays the import, prose-only answers never do.
const MonacoCodeView = lazy(() => import("./MonacoCodeView"));

/**
 * A fenced code block, shown in a read-only Monaco editor sized to its content.
 *
 * Until Monaco loads, the same text sits in a plain frame of the same height, so
 * nothing moves when the editor takes over.
 */
export function CodeBlock({ code, fence }: { code: string; fence?: string }) {
  const theme = useTheme();
  const { mode } = useThemeMode();
  const palette = useMemo(() => markdownPalette(theme, mode), [theme, mode]);
  const text = code.replace(/\n$/, "");
  const language = resolveCodeLanguage(fence, text);
  // Lines never wrap, so the height is known up front and the editor never resizes.
  const height = contentHeight(text);
  const fallback = (
    <pre css={fallbackStyles(theme, palette)}>
      {text}
    </pre>
  );

  return (
    <div data-md-code="" data-language={language.id}>
      <div data-md-code-head="">{language.label}</div>
      <div data-md-code-body="" style={{ height }}>
        <Suspense fallback={fallback}>
          <MonacoCodeView
            code={text}
            language={language.id}
            mode={mode}
            palette={palette}
            fontFamily={theme.font.mono}
            loading={fallback}
          />
        </Suspense>
      </div>
    </div>
  );
}

/** The editor height: lines plus its padding. */
function contentHeight(text: string): number {
  return text.split("\n").length * CODE_METRICS.lineHeight + CODE_METRICS.padding * 2;
}

function fallbackStyles(theme: Theme, palette: MarkdownPalette) {
  return css`
    margin: 0;
    height: 100%;
    overflow: hidden;
    padding: ${CODE_METRICS.padding}px 0 0 ${CODE_METRICS.gutter}px;
    font: ${CODE_METRICS.fontSize}px / ${CODE_METRICS.lineHeight}px ${theme.font.mono};
    white-space: pre;
    color: ${palette.body};
    background: ${palette.codeBg};
  `;
}
