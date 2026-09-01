import { useEffect, useId, useState } from "react";
import { css, useTheme } from "@emotion/react";
import type { Theme } from "@emotion/react";
import { useThemeMode } from "@/theme/themeMode";

/**
 * A fenced ` ```mermaid ` block, rendered as a diagram.
 *
 * Mermaid is a large dependency, so it is loaded lazily — the first diagram on
 * the page pays the import, and a conversation with no diagrams never does.
 * While the import and render are in flight the block falls back to the raw
 * source in a code frame, so a diagram that is slow to appear reads as a code
 * block rather than as a hole in the answer.
 *
 * The rendered SVG is inserted with `dangerouslySetInnerHTML`. That is the
 * integration mermaid itself documents, and it is safe here because mermaid
 * sanitises its own output (DOMPurify, `securityLevel: "strict"` by default)
 * before returning it — the same guarantee that lets the rest of the markdown
 * renderer refuse raw HTML in the first place.
 */
export function MermaidDiagram({ source }: { source: string }) {
  const theme = useTheme();
  const { mode } = useThemeMode();
  const id = useId();
  const [render, setRender] = useState<RenderState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;

    // `useId` can contain colons, which mermaid's id handling does not like.
    const diagramId = `mermaid-${id.replace(/[^a-zA-Z0-9_-]/g, "")}`;

    void import("mermaid")
      .then(({ default: mermaid }) => {
        mermaid.initialize({
          startOnLoad: false,
          theme: mode === "light" ? "neutral" : "dark",
          // The page's own font, so a diagram reads like the rest of the answer.
          fontFamily: theme.font.body,
        });
        return mermaid.render(diagramId, source);
      })
      .then(({ svg: rendered }) => {
        if (!cancelled) setRender({ status: "done", svg: rendered });
      })
      .catch(() => {
        if (!cancelled) setRender({ status: "failed" });
      });

    return () => {
      cancelled = true;
    };
  }, [source, mode, theme.font.body, id]);

  if (render.status === "failed") {
    return (
      <pre css={diagramFallbackStyles(theme)}>
        <code>{source}</code>
      </pre>
    );
  }

  if (render.status === "loading") {
    // Still importing or rendering: show the source so the block is never a gap.
    return (
      <pre css={diagramFallbackStyles(theme)}>
        <code>{source}</code>
      </pre>
    );
  }

  return (
    <div
      data-testid="chat-mermaid"
      css={diagramStyles(theme)}
      // The SVG is mermaid's own sanitised output; see the comment above.
      dangerouslySetInnerHTML={{ __html: render.svg }}
    />
  );
}

/** What the renderer has produced so far for the current source. */
type RenderState =
  | { status: "loading" }
  | { status: "done"; svg: string }
  | { status: "failed" };

/** The frame a diagram sits in: the same quiet box a code block gets. */
function diagramStyles(theme: Theme) {
  return css`
    margin: ${theme.space(2)} 0;
    padding: ${theme.space(3)};
    background: ${theme.color.bg};
    border: 1px solid ${theme.color.border};
    border-radius: ${theme.radius.md}px;
    overflow-x: auto;

    & svg {
      max-width: 100%;
      height: auto;
      display: block;
    }
  `;
}

/** The raw source, shown while the diagram is loading or when it failed. */
function diagramFallbackStyles(theme: Theme) {
  return css`
    margin: ${theme.space(2)} 0;
    padding: ${theme.space(3)};
    background: ${theme.color.bg};
    border: 1px solid ${theme.color.border};
    border-radius: ${theme.radius.md}px;
    overflow-x: auto;

    & code {
      font-family: ${theme.font.mono};
      font-size: 0.85em;
      background: none;
      border: none;
      padding: 0;
    }
  `;
}
