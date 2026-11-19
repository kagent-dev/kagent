import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import { css, useTheme } from "@emotion/react";
import type { Theme } from "@emotion/react";
import { useThemeMode } from "@/theme/useThemeMode";
import { inlineLink } from "@/components/common/linkStyles";
import { MermaidDiagram } from "./MermaidDiagram";
import { CodeBlock } from "./CodeBlock";
import { markdownPalette, type MarkdownPalette } from "./markdownPalette";

/**
 * GitHub-flavoured markdown (`remark-gfm`: tables, task lists, strikethrough,
 * autolinks) with single newlines kept as line breaks (`remark-breaks`).
 *
 * No raw-HTML plugin is added on purpose. Without `rehype-raw`, react-markdown
 * treats any HTML in the text as literal characters rather than markup, so a reply
 * cannot smuggle a `<script>` or an `on* ` handler onto the page. URLs are passed
 * through react-markdown's default transform, which already drops `javascript:`
 * and other dangerous schemes.
 */
const REMARK_PLUGINS = [remarkGfm, remarkBreaks];

type HastNode = { type: string; value?: string; children?: HastNode[] };

/** The plain text under a markdown AST node. */
function textOf(node: HastNode): string {
  if (node.type === "text") return node.value ?? "";
  return (node.children ?? []).map(textOf).join("");
}

/** A table's scrolling frame, marking which edges hide more columns so CSS can fade them. */
function TableFrame({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState("");
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const update = () => {
      const start = el.scrollLeft > 1;
      const end = el.scrollLeft + el.clientWidth < el.scrollWidth - 1;
      setEdges([start ? "start" : "", end ? "end" : ""].join(" ").trim());
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    el.addEventListener("scroll", update, { passive: true });
    return () => {
      observer.disconnect();
      el.removeEventListener("scroll", update);
    };
  }, []);
  return (
    <div data-md-table-frame="" data-more={edges || undefined}>
      <div data-md-table="" ref={ref}>
        {children}
      </div>
    </div>
  );
}

const COMPONENTS: Components = {
  a(props) {
    const { node, ...anchorProps } = props;
    // `node` is react-markdown's AST handle, not a DOM attribute. `noopener` stops
    // the opened page from navigating this one.
    void node;
    return <a {...anchorProps} target="_blank" rel="noopener noreferrer" />;
  },
  table(props) {
    const { node, ...tableProps } = props;
    void node;
    // The frame scrolls, so a wide table never widens the bubble.
    return (
      <TableFrame>
        <table {...tableProps} />
      </TableFrame>
    );
  },
  td(props) {
    const { node, ...cellProps } = props;
    // A single token (a name, an id, a number) never breaks across lines.
    const token = node ? !/\s/.test(textOf(node).trim()) : false;
    return <td {...cellProps} data-md-nowrap={token ? "" : undefined} />;
  },
  pre(props) {
    const { node, children } = props;
    const code = node?.children?.[0];
    if (node?.tagName !== "pre" || code?.type !== "element" || code.tagName !== "code") {
      return <pre>{children}</pre>;
    }
    const classes = Array.isArray(code.properties?.className) ? code.properties.className.map(String) : [];
    const fence = classes.find((name) => name.startsWith("language-"))?.slice("language-".length);
    const text = textOf(code as HastNode);
    // A `mermaid` fence is a diagram, not a listing.
    if (fence === "mermaid") return <MermaidDiagram source={text} />;
    return <CodeBlock code={text} fence={fence} />;
  },
};

/**
 * Agent prose, rendered as markdown.
 *
 * It carries no bubble of its own — the caller owns the padding, background and
 * border — and colours everything from the theme so it reads in both palettes.
 */
export function MarkdownMessage({ children }: { children: string }) {
  const theme = useTheme();
  const { mode } = useThemeMode();
  const palette = useMemo(() => markdownPalette(theme, mode), [theme, mode]);
  return (
    <div css={markdownStyles(theme, palette)}>
      <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={COMPONENTS}>
        {children}
      </ReactMarkdown>
    </div>
  );
}

const CHECK = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M3.5 8.5l3 3 6-7' fill='none' stroke='%23f9fafb' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E")`;

/** Task-list boxes drawn by hand: the browser greys out a disabled native one. */
function checkboxStyles(theme: Theme) {
  return css`
    & input[type="checkbox"] {
      appearance: none;
      width: 15px;
      height: 15px;
      margin: 0 0.6em 0 0;
      vertical-align: -0.15em;
      border: 1.5px solid ${theme.color.borderStrong};
      border-radius: 4px;
      background: ${theme.color.bgElevated};
    }
    & input[type="checkbox"]:checked {
      border-color: ${theme.color.primary};
      background: ${theme.color.primary} ${CHECK} center / 12px no-repeat;
    }
  `;
}

/** Editorial · Lavender: brand-tinted quotes, a diamond break, and lifted dark surfaces. */
function markdownStyles(theme: Theme, p: MarkdownPalette) {
  const { color } = theme;
  return css`
    /* Wide tables and code scroll inside their frames instead of widening the column. */
    contain: inline-size;
    font-size: 15px;
    line-height: 1.65;
    color: ${p.body};
    word-break: break-word;
    font-variant-numeric: tabular-nums;

    & > :first-child {
      margin-top: 0;
    }
    & > :last-child {
      margin-bottom: 0;
    }

    /* Prose keeps a ~68 character measure; tables and code use the full width. */
    & > p,
    & > ul,
    & > ol,
    & > blockquote,
    & > h1,
    & > h2,
    & > h3,
    & > h4 {
      max-width: 68ch;
    }

    & p {
      margin: 0 0 0.85em;
    }
    & strong {
      font-weight: 650;
      color: ${p.heading};
    }
    & del {
      color: ${p.muted};
    }

    /* More space above a heading than below, so it binds to what it introduces. */
    & h1,
    & h2,
    & h3,
    & h4,
    & h5,
    & h6 {
      color: ${p.heading};
      margin: 1.6em 0 0.5em;
    }
    & h1 {
      font-size: 22px;
      line-height: 1.25;
      font-weight: 700;
      letter-spacing: -0.015em;
    }
    & h2 {
      font-size: 18px;
      line-height: 1.3;
      font-weight: 650;
      letter-spacing: -0.01em;
      padding-bottom: 6px;
      border-bottom: 1px solid ${p.tableRule};
    }
    & h3 {
      font-size: 15.5px;
      line-height: 1.4;
      font-weight: 650;
    }
    & h4,
    & h5,
    & h6 {
      font-size: 12px;
      line-height: 1.4;
      font-weight: 700;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      color: ${p.muted};
    }

    & ul,
    & ol {
      margin: 0 0 0.85em;
      padding-inline-start: 1.5em;
    }
    & li {
      margin: 0.3em 0;
      padding-inline-start: 0.2em;
    }
    & li > p {
      margin: 0;
    }
    & li::marker {
      color: ${p.muted};
    }
    & ol > li::marker {
      font-weight: 600;
    }
    & li > ul,
    & li > ol {
      margin: 0.3em 0 0.4em;
      padding-inline-start: 1.25em;
    }
    & ul ul {
      list-style-type: circle;
    }
    & ul ul ul {
      list-style-type: square;
    }
    & ul.contains-task-list {
      list-style: none;
      padding-inline-start: 0.1em;
    }
    ${checkboxStyles(theme)}

    & a {
      ${inlineLink(theme)};
      text-underline-offset: 3px;
    }

    /* A tinted panel with a brand bar: unmistakably a quote. */
    & blockquote {
      margin: 1.1em 0;
      padding: 10px 16px;
      background: ${p.quoteBg};
      border-inline-start: 3px solid ${p.quoteBar};
      border-radius: 0 ${theme.radius.sm}px ${theme.radius.sm}px 0;
      color: ${p.quoteText};
    }
    & blockquote p {
      margin: 0 0 0.5em;
    }
    & blockquote > :last-child {
      margin-bottom: 0;
    }
    & blockquote blockquote {
      margin: 0.5em 0 0;
      padding: 2px 0 2px 14px;
      background: none;
      border-inline-start: 2px solid ${p.rule};
      border-radius: 0;
      font-size: 14px;
      color: ${p.muted};
    }

    & code {
      font-family: ${theme.font.mono};
      font-size: 0.86em;
      background: ${p.inlineCodeBg};
      border-radius: 5px;
      padding: 0.12em 0.38em;
    }
    & pre code {
      font-size: inherit;
      background: none;
      padding: 0;
    }

    & [data-md-code] {
      margin: 0.4em 0 1.1em;
      border: 1px solid ${p.codeBorder};
      border-radius: ${theme.radius.md}px;
      overflow: hidden;
      background: ${p.codeBg};
    }
    & [data-md-code-head] {
      padding: 6px 16px 0;
      font: 600 11px/16px ${theme.font.mono};
      letter-spacing: 0.04em;
      color: ${p.muted};
    }

    & [data-md-table-frame] {
      margin: 0.4em 0 1.1em;
    }
    & [data-md-table] {
      overflow-x: auto;
      border: 1px solid ${p.tableRule};
      border-radius: ${theme.radius.md}px;
    }
    /* Cells wrap at spaces only; an unbreakable name scrolls the frame instead. */
    & table {
      min-width: 100%;
      border-collapse: collapse;
      font-size: 13.5px;
      line-height: 1.5;
      word-break: normal;
      overflow-wrap: normal;
    }
    & td[style*="right"],
    & td[style*="center"] {
      white-space: nowrap;
    }
    & th,
    & td {
      padding: 9px 12px;
      text-align: start;
      vertical-align: top;
    }
    & th {
      font-size: 12.5px;
      font-weight: 650;
      color: ${p.heading};
      white-space: nowrap;
      background: ${p.tableHeadBg};
      border-bottom: 1px solid ${color.borderStrong};
    }
    & td {
      border-bottom: 1px solid ${p.tableRule};
    }
    & tbody tr:last-child td {
      border-bottom: none;
    }

    /* A break in thought: a rule fading out at both ends around a small diamond. */
    & hr {
      position: relative;
      height: 1px;
      margin: 2em 0;
      border: none;
      overflow: visible;
      background: linear-gradient(90deg, transparent, ${p.rule} 25%, ${p.rule} 75%, transparent);
    }
    & hr::after {
      content: "";
      position: absolute;
      left: 50%;
      top: 50%;
      width: 7px;
      height: 7px;
      background: ${p.ruleAccent};
      transform: translate(-50%, -50%) rotate(45deg);
      box-shadow: 0 0 0 6px ${color.bgElevated};
    }

    & img {
      display: block;
      max-width: 100%;
      border: 1px solid ${p.tableRule};
      border-radius: ${theme.radius.md}px;
    }
    & p:has(> img:only-child) {
      margin-bottom: 0.4em;
    }
    /* The line under an image reads as its caption. */
    & p:has(> img:only-child) + p {
      font-size: 13px;
      line-height: 1.5;
      color: ${p.muted};
    }

    /* Fade whichever edge hides more columns, so a cut-off table reads as scrollable. */
    & [data-md-table-frame] {
      position: relative;
    }
    & [data-md-table-frame]::before,
    & [data-md-table-frame]::after {
      content: "";
      position: absolute;
      top: 1px;
      bottom: 1px;
      width: 32px;
      pointer-events: none;
      opacity: 0;
    }
    & [data-md-table-frame]::before {
      inset-inline-start: 1px;
      border-radius: ${theme.radius.md}px 0 0 ${theme.radius.md}px;
      background: linear-gradient(to right, ${theme.color.bgElevated}, transparent);
    }
    & [data-md-table-frame]::after {
      inset-inline-end: 1px;
      border-radius: 0 ${theme.radius.md}px ${theme.radius.md}px 0;
      background: linear-gradient(to left, ${theme.color.bgElevated}, transparent);
    }
    & [data-md-table-frame][data-more~="start"]::before,
    & [data-md-table-frame][data-more~="end"]::after {
      opacity: 1;
    }
    & [data-md-nowrap] {
      white-space: nowrap;
    }
  `;
}
