import { useTheme } from "@emotion/react";
import { shortInstanceId } from "./instanceLabels";

/**
 * A muted bar standing in for a conversation's title while its read is in flight.
 * Inline and shorter than a line, so the row keeps its height; the hidden text names it.
 */
export function ConversationTitlePlaceholder({
  instanceId,
  width = "12em",
}: {
  instanceId: string;
  width?: string;
}) {
  const theme = useTheme();
  return (
    <span
      data-testid={`conversation-title-loading-${instanceId}`}
      css={{
        position: "relative",
        display: "inline-block",
        verticalAlign: "middle",
        width,
        maxWidth: "100%",
        height: "0.8em",
        borderRadius: 4,
        background: theme.color.border,
      }}
    >
      <span
        css={{
          position: "absolute",
          width: 1,
          height: 1,
          overflow: "hidden",
          clipPath: "inset(50%)",
          whiteSpace: "nowrap",
        }}
      >
        Loading title, conversation {shortInstanceId(instanceId)}
      </span>
    </span>
  );
}
