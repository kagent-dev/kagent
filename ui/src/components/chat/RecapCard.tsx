import { Typography } from "antd";
import { Sparkles } from "lucide-react";
import { useTheme } from "@emotion/react";
import type { ConversationRecap } from "@/api";
import { relativeAge } from "@/components/agent-instances/instanceLabels";

const { Text } = Typography;

/** The stored recap of a conversation, set apart from the replies around it. Clicking it collapses it. */
export function RecapCard({ recap, onDismiss }: { recap: ConversationRecap; onDismiss: () => void }) {
  const theme = useTheme();
  // A purple wash over the elevated surface, so it sits apart from the page and the replies in both themes.
  const wash = (percent: number) => `color-mix(in srgb, ${theme.color.primary} ${percent}%, ${theme.color.bgElevated})`;
  return (
    <button
      type="button"
      aria-expanded
      data-testid="chat-recap"
      onClick={onDismiss}
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
        <span>
          Recap from <time dateTime={recap.generatedAt}>{relativeAge(recap.generatedAt)}</time>
        </span>
      </Text>
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
    </button>
  );
}
