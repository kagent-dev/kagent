import { Empty, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Wrench } from "lucide-react";
import type { DiscoveredTool } from "@/api";
import { ResourceLink } from "@/components/common/ResourceLink";
import { mcpServerTo } from "@/router/routes";
import type { BackLinkState } from "@/router/backLink";
import { toolAnchor } from "./toolAnchor";

const { Text } = Typography;

interface ToolServerToolsProps {
  tools: DiscoveredTool[];
  /** Highlighted inside tool names and descriptions, when a filter is active. */
  highlight?: string;
  /** Links each tool name to its place on this server's details page. */
  server?: { namespace: string; name: string; groupKind: string };
  /** Router state for those links, so the details page leads back here. */
  linkState?: BackLinkState;
  /** Gives each tool an anchor id, and marks the one named here. */
  anchors?: { current?: string };
}

/**
 * The tools one server reported at handshake, shown when its row is expanded.
 *
 * A server with nothing here is a normal state, not a failure: the controller
 * may not have completed a handshake yet, or the server may genuinely expose
 * nothing. The copy says which without implying something went wrong.
 */
export function ToolServerTools({ tools, highlight, server, linkState, anchors }: ToolServerToolsProps) {
  const theme = useTheme();

  if (tools.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        data-testid="tool-server-no-tools"
        description="This server has not reported any tools yet."
      />
    );
  }

  return (
    <ul
      data-testid="tool-server-tools"
      css={{
        listStyle: "none",
        margin: 0,
        padding: 0,
        display: "grid",
        gridTemplateColumns: "repeat(auto-fill, minmax(320px, 1fr))",
        gap: theme.space(3),
      }}
    >
      {sortToolsByName(tools).map((tool) => (
        <li
          key={tool.name}
          id={anchors ? toolAnchor(tool.name) : undefined}
          data-testid="tool-server-tool"
          data-current={anchors?.current === tool.name || undefined}
          css={{
            display: "flex",
            gap: theme.space(3),
            padding: theme.space(3),
            scrollMarginTop: theme.space(6),
            border: `1px solid ${anchors?.current === tool.name ? theme.color.primaryText : theme.color.border}`,
            borderRadius: theme.radius.md,
            background: theme.color.bg,
          }}
        >
          <Wrench
            size={16}
            aria-hidden
            css={{ flexShrink: 0, marginTop: 2, color: theme.color.textMuted }}
          />
          <div css={{ minWidth: 0 }}>
            <div css={{ fontFamily: theme.font.mono, wordBreak: "break-word" }}>
              {server ? (
                <ResourceLink
                  to={mcpServerTo(server, toolAnchor(tool.name))}
                  state={linkState}
                  data-testid="tool-server-tool-link"
                >
                  <Highlighted text={tool.name} term={highlight} />
                </ResourceLink>
              ) : (
                <Highlighted text={tool.name} term={highlight} />
              )}
            </div>
            {tool.description ? (
              <Text
                css={{
                  display: "block",
                  marginTop: theme.space(1),
                  color: theme.color.textMuted,
                }}
              >
                <Highlighted text={tool.description} term={highlight} />
              </Text>
            ) : null}
          </div>
        </li>
      ))}
    </ul>
  );
}

/**
 * The tools in alphabetical order by name.
 *
 * The controller reports them in discovery order, which means nothing to someone
 * scanning a long list for one tool. Numeric-aware, so `tool_2` sorts before `tool_10`.
 */
function sortToolsByName(tools: DiscoveredTool[]): DiscoveredTool[] {
  return [...tools].sort(
    (a, b) =>
      a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" }) ||
      // Names that differ only in case still get one fixed order.
      a.name.localeCompare(b.name, undefined, { numeric: true }),
  );
}

/**
 * Wraps occurrences of `term` in a `<mark>`.
 *
 * Rendered as text nodes rather than interpolated HTML, so a tool description
 * coming off the cluster can never inject markup here.
 */
export function Highlighted({ text, term }: { text: string; term?: string }) {
  const needle = term?.trim();
  if (!needle) return <>{text}</>;

  const segments: Array<{ value: string; match: boolean }> = [];
  const haystack = text.toLowerCase();
  const lowered = needle.toLowerCase();
  let cursor = 0;

  for (;;) {
    const at = haystack.indexOf(lowered, cursor);
    if (at === -1) break;
    if (at > cursor) segments.push({ value: text.slice(cursor, at), match: false });
    segments.push({ value: text.slice(at, at + needle.length), match: true });
    cursor = at + needle.length;
  }
  if (cursor < text.length) segments.push({ value: text.slice(cursor), match: false });

  return (
    <>
      {segments.map((segment, index) =>
        segment.match ? (
          <mark
            // Segments have no identity beyond their position, and the list is
            // rebuilt wholesale whenever the term changes, so the index is stable
            // for as long as this render is on screen.
            key={index}
            css={{
              background: "rgba(124, 58, 237, 0.32)",
              color: "inherit",
              borderRadius: 2,
              padding: "0 1px",
            }}
          >
            {segment.value}
          </mark>
        ) : (
          <span key={index}>{segment.value}</span>
        ),
      )}
    </>
  );
}
