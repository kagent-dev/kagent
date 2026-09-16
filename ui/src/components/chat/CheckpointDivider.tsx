import type { KeyboardEvent } from "react";
import { Button, Popconfirm, Tooltip, Typography } from "antd";
import { Eraser, GitFork, Pencil, Save } from "lucide-react";
import { useTheme } from "@emotion/react";
import { useThemeMode } from "@/theme/themeMode";
import type { Checkpoint } from "@/api";
import { ExtensionSlot } from "@/appExtensions/ExtensionSlot";

const { Text } = Typography;

/**
 * The boundary a fork would cut along.
 *
 * Drawn under the last message of a checkpointed turn, because that is what the
 * boundary means: everything above it travels into a fork and nothing below it does.
 *
 * The three things a reader does to a boundary sit under it. The line itself opens the
 * record behind it, which is where the id, the state and the age are — none of which
 * belong in a transcript.
 *
 * The transcript adds the room around this; see `CHECKPOINT_GAP` there.
 */
export function CheckpointDivider({
  checkpointId,
  checkpoint,
  onOpen,
  onFork,
  onRename,
  onDelete,
}: {
  checkpointId: string;
  /**
   * The record behind the line, when the page has read it.
   *
   * Optional because the line is drawn from two sources — the controller's list and
   * what this page has saved since it loaded — and a boundary saved a moment ago is on
   * screen before the list that describes it comes back.
   */
  checkpoint?: Checkpoint;
  /** Opens this snapshot's record. Absent on a read-only surface, as the rest are. */
  onOpen?: () => void;
  /** Forks this boundary. */
  onFork?: () => void;
  /** Names this boundary, and with it the forks taken from it. */
  onRename?: () => void;
  /** Removes this boundary. */
  onDelete?: () => void;
}) {
  const theme = useTheme();
  const { mode } = useThemeMode();

  // Whatever it is called, which until somebody renames it is the name the controller
  // generated. The id is in the record behind the line; the line says one thing.
  const name = checkpoint?.name;
  const heading = name ? `Snapshot “${name}”` : "Snapshot";

  // One shape for all three, so the row reads as a set of controls rather than as one
  // button with others bolted beside it.
  const control = {
    height: 24,
    fontSize: 12,
    paddingInline: theme.space(2),
  } as const;

  const filled = {
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
  } as const;

  /*
   * Outlined in the accent's *text* colour rather than in `primary`.
   *
   * `primary` is one purple for both themes, and on the dark page an outline in it sits
   * at nearly the page's own lightness — a button drawn in the dark. `accentText` is the
   * token that already answers "this purple, legible on this background", pulled a
   * quarter of the way back toward `primary` so it reads as purple rather than as lilac.
   * On the light theme the two tokens are the same colour, so this changes nothing there.
   */
  const outlinePurple = `color-mix(in srgb, ${theme.color.accentText} 72%, ${theme.color.primary} 28%)`;
  const outlined = {
    ...control,
    background: "transparent",
    color: outlinePurple,
    borderColor: outlinePurple,
    "&:hover, &:focus-visible": {
      background: theme.color.accentBg,
      borderColor: outlinePurple,
      color: outlinePurple,
    },
    "&:active": { background: theme.color.accentBg, opacity: 0.85 },
  } as const;

  function openOnKey(event: KeyboardEvent<HTMLDivElement>) {
    if (!onOpen || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    onOpen();
  }

  return (
    <div
      data-testid={`chat-checkpoint-mark-${checkpointId}`}
      css={{
        display: "grid",
        gap: theme.space(3),
        /*
         * A tinted band, edged rather than brightened — and the one place here that
         * reads the mode rather than a token.
         *
         * The accent pair sits at different distances from its page on the two themes:
         * on dark, `accentBg` is several steps above a near-black page and needs mixing
         * down or a thread of these reads as a stack of cards; on light it is already
         * within a whisker of white, and the same mix erases it. One value cannot be
         * both, so each mode gets the strength that makes it a mark.
         */
        ...(mode === "dark"
          ? {
              background: `color-mix(in srgb, ${theme.color.accentBg} 45%, transparent)`,
              border: `1px solid color-mix(in srgb, ${theme.color.accentBorder} 45%, transparent)`,
            }
          : {
              background: theme.color.accentBg,
              border: `1px solid ${theme.color.accentBorder}`,
            }),
        borderRadius: theme.radius.md,
        padding: theme.space(3),
        // The label carries its own 8px of press target above it, so the foot needs the
        // same again to look evenly set rather than bottom-heavy.
        paddingBlockEnd: theme.space(5),
      }}
    >
      {/* The whole row is the way into the record — the rule as much as the words — so
          the press target is the thing the reader sees rather than the few characters
          in front of it. Hover and press are drawn on the row for the same reason, and
          the tooltip is what gives back the half of a long name the row clips. */}
      <Tooltip title={onOpen ? "Open snapshot details." : undefined} placement="top">
        <div
          {...(onOpen
            ? {
                role: "button" as const,
                tabIndex: 0,
                onClick: onOpen,
                onKeyDown: openOnKey,
                "aria-label": `${heading}. Open its record.`,
              }
            : { role: "separator" as const, "aria-label": "Snapshot" })}
          data-testid={`chat-checkpoint-open-${checkpointId}`}
          css={{
            display: "flex",
            alignItems: "center",
            gap: theme.space(2),
            color: theme.color.primaryText,
            fontSize: 12,
            borderRadius: theme.radius.sm,
            // Roomy, because the whole row is the press target: a hover that hugs the
            // words reads as a link rather than as the control it is.
            paddingInline: theme.space(2),
            paddingBlock: theme.space(2),
            ...(onOpen
              ? {
                  cursor: "pointer",
                  transition: "background 120ms ease",
                  // Lifted with the foreground rather than with `accentBg`, which the
                  // band already uses — a hover painted in the band's own colour is a
                  // hover you cannot see.
                  "&:hover, &:focus-visible": {
                    background: "color-mix(in srgb, currentColor 12%, transparent)",
                    "[data-checkpoint-heading]": { textDecoration: "underline" },
                  },
                  "&:active": {
                    background: "color-mix(in srgb, currentColor 20%, transparent)",
                  },
                }
              : {}),
          }}
        >
          <Save size={12} aria-hidden />
          {/* Clipped rather than wrapped so the rule stays a rule; the tooltip on the row
              is what gives a generated name's second half back. */}
          <Text
            data-checkpoint-heading
            data-testid="chat-checkpoint-label"
            css={{
              color: "inherit",
              fontSize: "inherit",
              fontWeight: 600,
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
              maxWidth: "70%",
            }}
          >
            Snapshot{name ? " " : null}
            {name ? (
              <Text
                data-testid={`chat-checkpoint-subtitle-${checkpointId}`}
                css={{
                  color: "inherit",
                  fontSize: 11,
                  fontFamily: theme.font.mono,
                  fontWeight: 400,
                }}
              >
                &ldquo;{name}&rdquo;
              </Text>
            ) : null}
          </Text>
        </div>
      </Tooltip>
      {onFork || onRename || onDelete ? (
        <div
          css={{
            display: "flex",
            alignItems: "center",
            // Roomy, because these are three separate decisions and a tight row reads
            // as one segmented control.
            gap: theme.space(3),
            flexWrap: "wrap",
            // Lined up with the label above, which the row insets by the same amount.
            paddingInlineStart: theme.space(2),
          }}
        >
          {onFork ? (
            <Tooltip title="Fork the chat from this snapshot." placement="bottom">
              <Button
                size="small"
                type="primary"
                data-testid={`chat-checkpoint-fork-${checkpointId}`}
                aria-label="Fork the chat from this snapshot."
                icon={<GitFork size={13} />}
                onClick={onFork}
                css={filled}
              >
                Fork
              </Button>
            </Tooltip>
          ) : null}
          {onRename ? (
            <Tooltip
              title="Name this snapshot, and the forks taken from it."
              placement="bottom"
            >
              <Button
                size="small"
                data-testid={`chat-checkpoint-rename-${checkpointId}`}
                aria-label="Name this snapshot, and the forks taken from it."
                icon={<Pencil size={13} />}
                onClick={onRename}
                css={outlined}
              >
                Rename
              </Button>
            </Tooltip>
          ) : null}
          {/* Confirmed, because the runtime behind the boundary goes with it and
              nothing brings it back. */}
          {onDelete ? (
            <Popconfirm
              title="Delete this snapshot?"
              description="The stored runtime goes with it and nothing brings it back. Chats already forked from here are kept."
              // Capped, or the one line of copy sets the popover's width and it spans
              // half the transcript.
              overlayStyle={{ maxWidth: 320 }}
              okText="Delete"
              okButtonProps={{
                danger: true,
                "data-testid": `chat-checkpoint-delete-confirm-${checkpointId}`,
              }}
              cancelText="Cancel"
              onConfirm={onDelete}
            >
              <Tooltip
                title="Delete this snapshot and the runtime stored with it."
                placement="bottom"
              >
                <Button
                  size="small"
                  danger
                  data-testid={`chat-checkpoint-delete-${checkpointId}`}
                  aria-label="Delete this snapshot and the runtime stored with it."
                  icon={<Eraser size={13} />}
                  // Outlined in red against Fork's fill: two of these are things the
                  // line is for, and this is the one that takes something away.
                  css={{
                    ...control,
                    "&:hover:not(:disabled), &:focus-visible:not(:disabled)": {
                      background: theme.color.dangerBg,
                      borderColor: theme.color.dangerBorder,
                    },
                    "&:active:not(:disabled)": {
                      background: theme.color.dangerBg,
                      opacity: 0.85,
                    },
                  }}
                >
                  Delete
                </Button>
              </Tooltip>
            </Popconfirm>
          ) : null}

          {/* Beside the boundary's own controls, for a product whose snapshots mean
              something this application does not know about. */}
          {checkpoint ? (
            <ExtensionSlot
              id="app_agents_agentChat_snapshotDivider_actions"
              context={{
                snapshotId: checkpoint.id,
                instanceId: checkpoint.agentInstanceId,
                label: heading,
              }}
            />
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
