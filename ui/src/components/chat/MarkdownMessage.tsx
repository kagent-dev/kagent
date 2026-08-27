import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import { css, useTheme } from "@emotion/react";
import type { Theme } from "@emotion/react";

/**
 * GitHub-flavoured markdown (`remark-gfm`: tables, task lists, strikethrough,
 * autolinks) with single newlines kept as line breaks (`remark-breaks`). The
 * second is what stops rendering from swallowing the line breaks the old plain
 * `white-space: pre-wrap` used to preserve — an agent that lays an answer out over
 * several lines still reads that way.
 *
 * No raw-HTML plugin is added on purpose. Without `rehype-raw`, react-markdown
 * treats any HTML in the text as literal characters rather than markup, so a reply
 * cannot smuggle a `<script>` or an `on* ` handler onto the page. URLs are passed
 * through react-markdown's default transform, which already drops `javascript:`
 * and other dangerous schemes.
 */
const REMARK_PLUGINS = [remarkGfm, remarkBreaks];

const COMPONENTS: Components = {
  a(props) {
    const { node, ...anchorProps } = props;
    // `node` is react-markdown's AST handle, not a DOM attribute — keep it off the
    // anchor. Links open away from the app, and `noopener` cuts `window.opener` so
    // the opened page cannot navigate this one.
    void node;
    return <a {...anchorProps} target="_blank" rel="noopener noreferrer" />;
  },
};

/**
 * Agent prose, rendered as markdown.
 *
 * A component of its own, rather than a `ReactMarkdown` inline in the message, so
 * the plugin list and the element styling are defined once and the message stays
 * about which part goes where. It carries no bubble of its own — the caller owns
 * the padding, background and border — and colours everything from the theme so it
 * reads in both the light and dark palettes.
 */
export function MarkdownMessage({ children }: { children: string }) {
  const theme = useTheme();
  return (
    <div css={markdownStyles(theme)}>
      <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={COMPONENTS}>
        {children}
      </ReactMarkdown>
    </div>
  );
}

/** The look of rendered markdown inside a chat bubble: tight, and theme-coloured. */
function markdownStyles(theme: Theme) {
  const code = `font-family: ${theme.font.mono}; font-size: 0.85em;`;

  return css`
    line-height: 1.6;
    word-break: break-word;

    /* The bubble owns the outer padding, so the first and last blocks sit flush
       against it rather than adding a second gap inside the first. */
    & > :first-of-type {
      margin-top: 0;
    }
    & > :last-child {
      margin-bottom: 0;
    }

    & p {
      margin: ${theme.space(2)} 0;
    }

    & h1,
    & h2,
    & h3,
    & h4,
    & h5,
    & h6 {
      margin: ${theme.space(3)} 0 ${theme.space(2)};
      font-weight: 600;
      line-height: 1.3;
    }
    & h1 {
      font-size: 1.3em;
    }
    & h2 {
      font-size: 1.2em;
    }
    & h3 {
      font-size: 1.1em;
    }
    & h4,
    & h5,
    & h6 {
      font-size: 1em;
    }

    & ul,
    & ol {
      margin: ${theme.space(2)} 0;
      padding-inline-start: ${theme.space(5)};
    }
    & li {
      margin: ${theme.space(1)} 0;
    }
    & li > ul,
    & li > ol {
      margin: 0;
    }
    /* A task list from remark-gfm: its bullet is the checkbox, so drop the marker. */
    & li:has(> input[type="checkbox"]) {
      list-style: none;
      margin-inline-start: -${theme.space(4)};
    }
    & input[type="checkbox"] {
      margin-inline-end: ${theme.space(2)};
    }

    & a {
      color: ${theme.color.primary};
      text-decoration: underline;
    }

    & code {
      ${code}
      background: ${theme.color.bg};
      border: 1px solid ${theme.color.border};
      border-radius: ${theme.radius.sm}px;
      padding: 0.1em 0.35em;
    }
    /* A fenced block: the frame is the pre, so the code inside it drops the inline
       pill styling and just supplies the monospace run. */
    & pre {
      margin: ${theme.space(2)} 0;
      padding: ${theme.space(3)};
      background: ${theme.color.bg};
      border: 1px solid ${theme.color.border};
      border-radius: ${theme.radius.md}px;
      overflow-x: auto;
    }
    & pre code {
      ${code}
      background: none;
      border: none;
      border-radius: 0;
      padding: 0;
    }

    & blockquote {
      margin: ${theme.space(2)} 0;
      padding-inline-start: ${theme.space(3)};
      border-inline-start: 3px solid ${theme.color.border};
      color: ${theme.color.textMuted};
    }

    /* Wide tables scroll inside their own frame rather than stretching the bubble. */
    & table {
      display: block;
      overflow-x: auto;
      width: max-content;
      max-width: 100%;
      margin: ${theme.space(2)} 0;
      border-collapse: collapse;
    }
    & th,
    & td {
      padding: ${theme.space(1)} ${theme.space(2)};
      border: 1px solid ${theme.color.border};
      text-align: start;
    }
    & th {
      background: ${theme.color.bg};
      font-weight: 600;
    }

    & hr {
      margin: ${theme.space(3)} 0;
      border: none;
      border-top: 1px solid ${theme.color.border};
    }
    & img {
      max-width: 100%;
    }
  `;
}
