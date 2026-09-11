import { Button, Popconfirm, Tooltip, Typography } from "antd";
import { Eraser, GitFork, Save } from "lucide-react";
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
  onDelete,
}: {
  checkpointId: string;
  /** Forks this boundary. Absent on a read-only surface, which leaves the line alone. */
  onFork?: () => void;
  /** Removes this boundary. Absent on a read-only surface, as `onFork` is. */
  onDelete?: () => void;
}) {
  const theme = useTheme();
  // The two controls are drawn the same way, so the line does not read as one button
  // with something else bolted beside it.
  const control = {
    height: 24,
    fontSize: 12,
    paddingInline: theme.space(2),
    background: "transparent",
  } as const;
  const rule = (
    <span
      aria-hidden
      css={{ width: 14, height: 1, background: theme.color.primaryText, opacity: 0.4 }}
    />
  );

  return (
    <div
      data-testid={`chat-checkpoint-mark-${checkpointId}`}
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
      {/* Outlined, and named: on the line beside the label a bare icon left what it
          does to a hover, and a filled button pulled the eye off the conversation. One
          word — the tooltip says where from, so the line stays a line. */}
      {onFork || onDelete ? (
        <>
          {/* The rule carrying on between each pair, so the mark and the controls read
              as things on one line rather than a label with buttons stuck to it. */}
          {rule}
          {onFork ? (
            <Tooltip title="Fork the chat from this checkpoint" placement="bottom">
              <Button
                size="small"
                data-testid={`chat-checkpoint-fork-${checkpointId}`}
                aria-label="Fork the chat from this checkpoint"
                type="primary"
                icon={<GitFork size={13} />}
                onClick={onFork}
                css={{
                  ...control,
                  background: theme.color.primary,
                  color: theme.color.textOnPrimary,
                  borderColor: theme.color.primary,
                  "&:hover, &:focus-visible": {
                    background: theme.color.primaryHover,
                    borderColor: theme.color.primaryHover,
                    color: theme.color.textOnPrimary,
                  },
                  "&:active": { background: theme.color.primaryHover, opacity: 0.85 },
                }}
              >
                Fork
              </Button>
            </Tooltip>
          ) : null}
          {onFork && onDelete ? rule : null}
          {/* Confirmed, because the snapshot behind the boundary goes with it and
              nothing brings it back. */}
          {onDelete ? (
            <Popconfirm
              title="Remove this checkpoint?"
              description="The snapshot behind it will be deleted. This chat history and chat sessions already forked from here will be kept."
              // Capped, or the one line of copy sets the popover's width and it spans
              // half the transcript.
              overlayStyle={{ maxWidth: 300 }}
              okText="Remove"
              okButtonProps={{
                "data-testid": `chat-checkpoint-delete-confirm-${checkpointId}`,
              }}
              cancelText="Cancel"
              onConfirm={onDelete}
            >
              <Tooltip title="Remove this checkpoint. This deletes the associated snapshot." placement="bottom">
                <Button
                  size="small"
                  data-testid={`chat-checkpoint-delete-${checkpointId}`}
                  aria-label="Remove this checkpoint. This deletes the associated snapshot."
                  icon={<Eraser size={13} />}
                  // Outlined against Fork's fill: forking is what the line is for, and
                  // this is the one that takes something away.
                  css={{
                    ...control,
                    color: theme.color.primaryText,
                    borderColor: theme.color.primaryText,
                    "&:hover, &:focus-visible": {
                      color: theme.color.primaryText,
                      borderColor: theme.color.primaryText,
                      background: theme.color.accentBg,
                    },
                    "&:active": { background: theme.color.accentBg, opacity: 0.85 },
                  }}
                >
                  Remove
                </Button>
              </Tooltip>
            </Popconfirm>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
