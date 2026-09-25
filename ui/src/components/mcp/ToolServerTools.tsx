import { Empty, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Wrench } from "lucide-react";
import type { DiscoveredTool } from "@/api";

const { Text } = Typography;

interface ToolServerToolsProps {
  tools: DiscoveredTool[];
  /** Highlighted inside tool names and descriptions, when a filter is active. */
  highlight?: string;
}

/**
 * The tools one server reported at handshake, shown when its row is expanded.
 *
 * A server with nothing here is a normal state, not a failure: the controller
 * may not have completed a handshake yet, or the server may genuinely expose
 * nothing. The copy says which without implying something went wrong.
 */
export function ToolServerTools({ tools, highlight }: ToolServerToolsProps) {
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
      {tools.map((tool) => (
        <li
          key={tool.name}
          data-testid="tool-server-tool"
          css={{
            display: "flex",
            gap: theme.space(3),
            padding: theme.space(3),
            border: `1px solid ${theme.color.border}`,
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
              <Highlighted text={tool.name} term={highlight} />
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
