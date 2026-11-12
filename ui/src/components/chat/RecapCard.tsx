import { Typography } from "antd";
import { ChevronDown, ChevronRight, Sparkles } from "lucide-react";
import { useTheme } from "@emotion/react";
import type { ConversationRecap } from "@/api";
import { recapSpan } from "./recapSpan";

const { Text } = Typography;

/** The stored recap of a conversation: a quiet line that collapses to its header when clicked. */
export function RecapCard({
  recap,
  startedAt,
  collapsed,
  onToggle,
}: {
  recap: ConversationRecap;
  /** When the conversation began, so the header can say how much the recap covers. */
  startedAt?: string;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const theme = useTheme();
  // A purple wash over the elevated surface, so it sits apart from the page and the replies in both themes.
  const wash = (percent: number) => `color-mix(in srgb, ${theme.color.primary} ${percent}%, ${theme.color.bgElevated})`;
  const span = recapSpan(startedAt, recap.generatedAt);
  const Chevron = collapsed ? ChevronRight : ChevronDown;
  return (
    <button
      type="button"
      aria-expanded={!collapsed}
      data-testid="chat-recap"
      onClick={onToggle}
      css={{
        display: "grid",
        gap: theme.space(2),
        width: "100%",
        minWidth: 0,
        textAlign: "start",
        font: "inherit",
        cursor: "pointer",
        background: wash(10),
        border: `1px solid ${theme.color.accentBorder}`,
        borderRadius: theme.radius.md,
        padding: theme.space(3),
        "&:hover": { background: wash(16) },
        "&:active": { background: wash(22) },
      }}
    >
      <Text
        css={{
          display: "flex",
          alignItems: "center",
          gap: theme.space(2),
          color: theme.color.accentText,
          fontSize: 12,
          fontWeight: 600,
        }}
      >
        <Sparkles size={12} aria-hidden />
        <time dateTime={recap.generatedAt}>{span ? `Recap of ${span}` : "Recap"}</time>
        <Chevron size={14} aria-hidden css={{ marginInlineStart: "auto" }} />
      </Text>
      {collapsed ? null : (
        <Text
          css={{
            // Softer than the replies, still well above 4.5:1 in both themes.
            color: `color-mix(in srgb, ${theme.color.text} 80%, transparent)`,
            overflowWrap: "anywhere",
            whiteSpace: "pre-wrap",
            fontStyle: "italic",
          }}
        >
          {recap.text}
        </Text>
      )}
    </button>
  );
}
