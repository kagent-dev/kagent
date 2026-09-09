import { Button, Tooltip, Typography } from "antd";
import { GitFork, Save } from "lucide-react";
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
 * The transcript adds the room around this; see `CHECKPOINT_GAP` there.
 */
export function CheckpointDivider({
  checkpointId,
  onFork,
}: {
  checkpointId: string;
  /** Forks this boundary. Absent on a read-only surface, which leaves the line alone. */
  onFork?: () => void;
}) {
  const theme = useTheme();

  return (
    <div
      data-testid={`chat-checkpoint-${checkpointId}`}
      role="separator"
      aria-label="Checkpoint"
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
          // heading; the label and the button carry the colour at full strength.
          opacity: 0.4,
        },
      }}
    >
      <Save size={12} aria-hidden />
      <Text
        data-testid="chat-checkpoint-label"
        css={{ color: "inherit", fontSize: "inherit", fontWeight: 600 }}
      >
        Checkpoint
      </Text>
      {onFork ? (
        <Tooltip title="Start a new chat holding everything above this line.">
          <Button
            type="text"
            size="small"
            data-testid={`chat-checkpoint-fork-${checkpointId}`}
            icon={<GitFork size={13} />}
            onClick={onFork}
            css={{
              color: theme.color.primaryText,
              fontSize: 12,
              height: 22,
              paddingInline: theme.space(2),
              "&:hover, &:focus-visible": {
                color: theme.color.primaryText,
                background: theme.color.accentBg,
              },
            }}
          >
            Fork chat from here
          </Button>
        </Tooltip>
      ) : null}
    </div>
  );
}
