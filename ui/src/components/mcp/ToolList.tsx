import type { ReactElement, ReactNode } from "react";
import { Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Link, type To } from "react-router-dom";
import { linkInk } from "@/components/common/linkStyles";
import type { BackLinkState } from "@/router/backLink";

const { Paragraph } = Typography;

export interface ToolListItem {
  name: string;
  /** Undefined when the server has not reported the tool. */
  description?: string;
  /** Where the tool comes from, when the list mixes servers. */
  source?: string;
}

interface ToolListProps {
  tools: readonly ToolListItem[];
  /** Marked inside names and descriptions. */
  highlight?: readonly string[];
  /** Where a tool's name links to; no link when it returns undefined. */
  toolTo?: (tool: ToolListItem) => To | undefined;
  linkState?: BackLinkState;
  /** Called when a name link is followed. */
  onSelect?: (name: string) => void;
  /** 12px type, for side panels and table cells. */
  dense?: boolean;
  /** For each `li`. */
  itemTestId?: string;
  "data-testid"?: string;
  className?: string;
}

/** Tools, one per row and sorted by name: the name, and its description cut to two lines. */
export function ToolList({
  tools,
  highlight: terms,
  toolTo,
  linkState,
  onSelect,
  dense,
  itemTestId = "tool-list-item",
  "data-testid": testId = "tool-list",
  className,
}: ToolListProps) {
  const theme = useTheme();
  const size = dense ? 12 : 13;

  return (
    <ul
      data-testid={testId}
      className={className}
      css={{ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: theme.space(2), minWidth: 0 }}
    >
      {sortToolsByName(tools).map((tool) => {
        const name = <Highlighted text={tool.name} terms={terms} />;
        const to = toolTo?.(tool);
        return (
          <li
            key={`${tool.source ?? ""}|${tool.name}`}
            data-testid={itemTestId}
            css={{ display: "grid", minWidth: 0, fontSize: size, lineHeight: "18px" }}
          >
            <span css={{ fontFamily: theme.font.mono, overflowWrap: "anywhere", color: theme.color.text }}>
              {to ? (
                <Link
                  to={to}
                  state={linkState}
                  onClick={() => onSelect?.(tool.name)}
                  data-testid="tool-list-link"
                  css={{
                    ...linkInk(theme),
                    borderRadius: 2,
                    "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 2 },
                  }}
                >
                  {name}
                </Link>
              ) : (
                name
              )}
              {tool.source ? (
                <span css={{ color: theme.color.textMuted, fontFamily: theme.font.body, marginInlineStart: theme.space(2) }}>
                  {tool.source}
                </span>
              ) : null}
            </span>
            <ClampedText size={size}>
              {tool.description ? highlight(tool.description, terms) : "No description reported."}
            </ClampedText>
          </li>
        );
      })}
    </ul>
  );
}

/** Muted text cut to two lines, with more/less when there is more to see. */
export function ClampedText({ children, size = 12 }: { children: ReactNode; size?: number }) {
  const theme = useTheme();
  return (
    <Paragraph
      css={{ "&&": { margin: 0, fontSize: size, lineHeight: "18px", color: theme.color.textMuted } }}
      ellipsis={{ rows: 2, expandable: "collapsible", symbol: (expanded) => (expanded ? "less" : "more") }}
    >
      {children}
    </Paragraph>
  );
}

/**
 * The tools in alphabetical order by name. Numeric-aware, so `tool_2` sorts before
 * `tool_10`; names that differ only in case still get one fixed order.
 */
function sortToolsByName<T extends ToolListItem>(tools: readonly T[]): T[] {
  return [...tools].sort(
    (a, b) =>
      a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" }) ||
      a.name.localeCompare(b.name, undefined, { numeric: true }) ||
      (a.source ?? "").localeCompare(b.source ?? ""),
  );
}

/** `text` with each occurrence of any of `terms` in a `<mark>`. */
export function Highlighted({ text, terms }: { text: string; terms?: readonly string[] }) {
  return <>{highlight(text, terms)}</>;
}

/**
 * The same as plain strings and marks rather than one element, so a clamped paragraph can
 * still cut the text. Text nodes rather than HTML, so a description cannot inject markup.
 */
function highlight(text: string, terms?: readonly string[]): Array<string | ReactElement> {
  const needles = (terms ?? []).map((term) => term.trim().toLowerCase()).filter(Boolean);
  const nodes: Array<string | ReactElement> = [];
  const haystack = text.toLowerCase();
  let cursor = 0;
  for (;;) {
    let at = -1;
    let length = 0;
    for (const needle of needles) {
      const found = haystack.indexOf(needle, cursor);
      if (found !== -1 && (at === -1 || found < at || (found === at && needle.length > length))) {
        at = found;
        length = needle.length;
      }
    }
    if (at === -1) break;
    if (at > cursor) nodes.push(text.slice(cursor, at));
    nodes.push(
      // Positional keys: the list is rebuilt whenever the terms change.
      <mark key={at} css={{ background: "rgba(124, 58, 237, 0.32)", color: "inherit", borderRadius: 2, padding: "0 1px" }}>
        {text.slice(at, at + length)}
      </mark>,
    );
    cursor = at + length;
  }
  if (cursor < text.length) nodes.push(text.slice(cursor));
  return nodes;
}
