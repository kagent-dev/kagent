import { Button, Tooltip, Typography } from "antd";
import { Save } from "lucide-react";
import { useTheme } from "@emotion/react";

const { Text } = Typography;

/**
 * The line a fork would cut along.
 *
 * Drawn under the last message of a checkpointed turn, because that is what the
 * boundary means: everything above it travels into a fork and nothing below it does.
 * A rule rather than a panel around the turn — a conversation can hold several of
 * these, and stacked boxes read as unrelated cards rather than as one thread with
 * marks in it.
 *
 * A rule, a label and a way in, and nothing else. The Fork and Remove buttons that
 * used to sit here made a mark on the conversation into a toolbar, and left nowhere
 * to put a third action; they live in the details modal the label opens.
 *
 * The transcript adds the room around this; see `CHECKPOINT_GAP` there.
 */
export function CheckpointDivider({
  checkpointId,
  onOpen,
}: {
  checkpointId: string;
  /** Opens this snapshot's details. Absent on a read-only surface, which leaves a plain line. */
  onOpen?: () => void;
}) {
  const theme = useTheme();

  return (
    <div
      data-testid={`chat-checkpoint-mark-${checkpointId}`}
      role="separator"
      aria-label="Snapshot"
      css={{
        display: "flex",
        alignItems: "center",
        gap: theme.space(2),
        color: theme.color.primaryText,
        fontSize: 12,
        // The rule is the element, so it is drawn as two halves either side of the
        // label rather than as a border something else sits on top of.
        "&::before, &::after": {
          content: '""',
          flex: 1,
          height: 1,
          background: theme.color.primaryText,
          // Quiet enough to read as a mark on the conversation rather than a section
          // heading; the label carries the colour at full strength.
          opacity: 0.4,
        },
      }}
    >
      <Save size={12} aria-hidden />
      {onOpen ? (
        <Tooltip title="Open this snapshot: fork, rename or delete it." placement="bottom">
          <Button
            type="text"
            size="small"
            data-testid="chat-checkpoint-label"
            aria-label="Open this snapshot: fork, rename or delete it."
            onClick={onOpen}
            css={{
              height: 24,
              paddingInline: theme.space(2),
              color: theme.color.primaryText,
              fontSize: 12,
              fontWeight: 600,
              background: "transparent",
              // Underlined on hover as well as filled: on a line of muted rules a
              // background alone is a faint wash, and this is the only thing here
              // that can be pressed.
              "&:hover, &:focus-visible": {
                color: theme.color.primaryText,
                background: theme.color.accentBg,
                textDecoration: "underline",
              },
              "&:active": { background: theme.color.accentBg, opacity: 0.85 },
            }}
          >
            Snapshot
          </Button>
        </Tooltip>
      ) : (
        <Text
          data-testid="chat-checkpoint-label"
          css={{ color: "inherit", fontSize: "inherit", fontWeight: 600 }}
        >
          Snapshot
        </Text>
      )}
    </div>
  );
}
