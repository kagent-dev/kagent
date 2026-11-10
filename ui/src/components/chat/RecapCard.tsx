import { Typography } from "antd";
import { Sparkles, X } from "lucide-react";
import { useTheme } from "@emotion/react";
import type { ConversationRecap } from "@/api";
import { relativeAge } from "@/components/agent-instances/instanceLabels";

const { Text } = Typography;

/** The stored recap of a conversation, set apart from the replies around it. */
export function RecapCard({ recap, onDismiss }: { recap: ConversationRecap; onDismiss: () => void }) {
  const theme = useTheme();
  return (
    <section
      aria-label="Recap"
      data-testid="chat-recap"
      css={{
        display: "grid",
        gap: theme.space(2),
        // Tinted with a readable edge, so it reads as a note beside the chat, not a reply.
        background: theme.color.accentBg,
        border: `1px solid ${theme.color.borderStrong}`,
        borderRadius: theme.radius.md,
        padding: theme.space(3),
        minWidth: 0,
      }}
    >
      <div css={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
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
          Recap
        </Text>
        <button
          type="button"
          aria-label="Dismiss recap"
          onClick={onDismiss}
          css={{
            display: "inline-grid",
            placeItems: "center",
            padding: 2,
            border: 0,
            borderRadius: theme.radius.sm,
            background: "transparent",
            color: theme.color.textMuted,
            cursor: "pointer",
            "&:hover": { color: theme.color.text, background: theme.color.accentBg },
            "&:active": { transform: "scale(0.92)" },
          }}
        >
          <X size={14} aria-hidden />
        </button>
      </div>
      <Text css={{ color: theme.color.text, overflowWrap: "anywhere", whiteSpace: "pre-wrap", fontStyle: "italic" }}>{recap.text}</Text>
      <time dateTime={recap.generatedAt} css={{ color: theme.color.textMuted, fontSize: 12 }}>
        Written {relativeAge(recap.generatedAt)}
      </time>
    </section>
  );
}
