import { useId, useState } from "react";
import { Tooltip } from "antd";
import { useTheme } from "@emotion/react";
import { ChevronRight, ShieldAlert } from "lucide-react";
import { DEFAULT_SERVER_KIND, type ToolRow, type ToolServerGroup as Group } from "./groupMcpTools";

const ASKS_FIRST = "Asks before running";

function ApprovalIcon({ label, testId }: { label: string; testId: string }) {
  const theme = useTheme();
  return (
    <Tooltip title={label}>
      <span
        role="img"
        aria-label={label}
        tabIndex={0}
        data-testid={testId}
        css={{
          display: "inline-flex",
          flexShrink: 0,
          color: theme.color.warningText,
          borderRadius: theme.radius.sm,
          "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 2 },
        }}
      >
        <ShieldAlert size={16} aria-hidden />
      </span>
    </Tooltip>
  );
}

function serverIconLabel(group: Group): string | undefined {
  if (group.approval === "all") return "Every tool asks first";
  if (group.approval === "some") {
    const parts = [
      group.askCount > 0 && `${group.askCount} ask first`,
      group.freeCount > 0 && `${group.freeCount} run without asking`,
      group.otherAsks !== undefined &&
        `all others ${group.otherAsks ? "ask first" : "run without asking"}`,
    ];
    return parts.filter(Boolean).join(", ");
  }
  return undefined;
}

function countText(group: Group): string {
  const { namedCount } = group;
  const hasAll = group.rows.some((row) => row.kind !== "tool");
  if (namedCount === 0) return "All tools";
  const named = `${namedCount} ${namedCount === 1 ? "tool" : "tools"}`;
  return hasAll ? `${named} and all others` : named;
}

function rowName(row: ToolRow): string {
  if (row.kind === "all") return "All tools";
  if (row.kind === "allOther") return "All other tools";
  return row.name;
}

/** One MCP server: its tools as rows, an icon on those that ask first, and an expand control. */
export function ToolServerGroup({ group }: { group: Group }) {
  const theme = useTheme();
  const listId = useId();
  const [open, setOpen] = useState(false);
  const iconLabel = serverIconLabel(group);
  const mono = { fontFamily: theme.font.mono, fontSize: 12.5 };
  // Ids and labels name the kind only when it is not the default, so the usual ones stay short.
  const id = group.kind === DEFAULT_SERVER_KIND ? group.server : `${group.kind}/${group.server}`;

  return (
    <div
      data-testid={`structure-tool-server-${id}`}
      css={{
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.md,
        background: theme.color.bg,
        minWidth: 0,
      }}
    >
      <div css={{ display: "flex", alignItems: "center", gap: theme.space(2) }}>
        <button
          type="button"
          aria-expanded={open}
          aria-controls={listId}
          aria-label={`Show tools for ${id}`}
          data-testid={`structure-tool-server-${id}-toggle`}
          onClick={() => setOpen((value) => !value)}
          css={{
            flex: 1,
            minWidth: 0,
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            gap: theme.space(2),
            padding: `${theme.space(2)} ${theme.space(3)}`,
            font: "inherit",
            color: theme.color.text,
            textAlign: "left",
            background: "none",
            border: 0,
            borderRadius: theme.radius.md,
            cursor: "pointer",
            "&:hover": { background: `${theme.color.text}14` },
            "&:active": { background: `${theme.color.primary}33` },
            "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: -2 },
          }}
        >
          <ChevronRight
            size={16}
            aria-hidden
            css={{
              flexShrink: 0,
              color: theme.color.textMuted,
              transition: "transform 120ms",
              transform: open ? "rotate(90deg)" : undefined,
              "@media (prefers-reduced-motion: reduce)": { transition: "none" },
            }}
          />
          <span css={{ ...mono, fontWeight: 600, wordBreak: "break-word" }}>{group.server}</span>
          {id !== group.server ? (
            <span css={{ color: theme.color.textMuted, fontSize: 12 }}>{group.kind}</span>
          ) : null}
          <span css={{ color: theme.color.textMuted, fontSize: 13 }}>{countText(group)}</span>
        </button>
        {iconLabel ? (
          <span css={{ paddingInlineEnd: theme.space(3), display: "inline-flex" }}>
            <ApprovalIcon label={iconLabel} testId="structure-tool-server-approval" />
          </span>
        ) : null}
      </div>
      {open ? (
        <ul
          id={listId}
          css={{
            listStyle: "none",
            margin: 0,
            padding: `0 ${theme.space(3)} ${theme.space(2)} ${theme.space(9)}`,
            display: "grid",
            gap: theme.space(1),
          }}
        >
          {group.rows.map((row) => (
            <li
              key={`${row.kind}:${row.name}`}
              data-testid={`structure-tool-row-${row.kind === "tool" ? row.name : row.kind}`}
              css={{ display: "flex", alignItems: "center", gap: theme.space(2), minWidth: 0 }}
            >
              <span
                css={{
                  ...mono,
                  wordBreak: "break-word",
                  ...(row.kind === "tool" ? {} : { fontFamily: "inherit", fontSize: 13 }),
                }}
              >
                {rowName(row)}
              </span>
              {row.requiresApproval ? (
                <ApprovalIcon
                  label={row.kind === "all" ? "All of these ask first" : ASKS_FIRST}
                  testId="structure-tool-approval"
                />
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
