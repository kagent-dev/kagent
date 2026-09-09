import { Button, Dropdown, Tooltip, Typography } from "antd";
import { GitFork, MoreVertical, Save } from "lucide-react";
import { useTheme } from "@emotion/react";
import { ExtensionSlot } from "@/appExtensions";
import type { ChatMessage } from "@/api";
import { ToolCallCard } from "./ToolCallCard";
import { MarkdownMessage } from "./MarkdownMessage";
import { isAwaitingContent, messageText } from "./messageText";

const { Text } = Typography;

/**
 * One message: prose from the user or the agent, or a tool call and its result.
 *
 * A message can hold several parts, so this renders each part in order rather
 * than picking one shape per message — a turn that calls a tool and then
 * explains itself is one message in the transport's terms.
 */
export function ChatMessageItem({
  message,
  sessionId,
  onCheckpoint,
  canCheckpoint = false,
  isCheckpointed = false,
  isCheckpointing = false,
  onFork,
}: {
  message: ChatMessage;
  /** The conversation this message belongs to, for the per-message extension point. */
  sessionId?: string;
  /**
   * Saves this message as a turn boundary. Drawn only on the reader's own messages,
   * and only when a surface provides it — so a read-only view has no control that
   * would be refused.
   */
  onCheckpoint?: () => void;
  /**
   * Whether a checkpoint can be taken here.
   *
   * The reader's latest message only: `CreateCheckpoint` takes no cutoff, so the
   * boundary it saves is always the conversation's current one. Saving an earlier
   * message means having saved it while it was the latest.
   */
  canCheckpoint?: boolean;
  /** Whether a boundary is already saved at this message. */
  isCheckpointed?: boolean;
  isCheckpointing?: boolean;
  /**
   * Forks the conversation from this message's checkpoint. Offered on the reader's
   * own messages and enabled only once one is saved here.
   */
  onFork?: () => void;
}) {
  const theme = useTheme();
  const isUser = message.role === "user";
  const text = messageText(message);

  return (
    <article
      data-testid="chat-message"
      data-message-id={message.id}
      data-role={message.role}
      data-checkpointed={isCheckpointed || undefined}
      css={{
        display: "grid",
        gap: theme.space(2),
        justifyItems: isUser ? "end" : "start",
        /*
         * A checkpointed message is marked by the panel around it, not by a ring on
         * the bubble: the reader's bubble is already the primary purple and a ring
         * drawn on it disappears into that purple on the light theme.
         *
         * `primaryText` for the edge rather than `accentBorder`, which measures 1.2:1
         * against the light page and is not a visible boundary there; this one clears
         * 6:1 on both. The panel is an elevated surface, so the tint reads on the dark
         * theme without needing a colour of its own on the light one — and the label
         * beside "You" says the same thing in words, so the mark never rests on colour.
         */
        ...(isCheckpointed
          ? {
              padding: theme.space(2),
              borderRadius: theme.radius.md,
              border: `1px solid ${theme.color.primaryText}`,
              background: theme.color.bgElevated,
            }
          : {}),
      }}
    >
      <div
        css={{
          display: "flex",
          alignItems: "center",
          gap: theme.space(2),
          color: theme.color.textMuted,
          fontSize: 12,
        }}
      >
        <Text css={{ color: "inherit", fontSize: "inherit" }}>
          {isUser ? "You" : "Agent"}
        </Text>
        {isCheckpointed ? (
          <span
            data-testid={`chat-message-checkpointed-${message.id}`}
            css={{
              display: "inline-flex",
              alignItems: "center",
              gap: theme.space(1),
              color: theme.color.primaryText,
              fontSize: "inherit",
              fontWeight: 600,
            }}
          >
            <Save size={12} aria-hidden />
            Checkpointed
          </span>
        ) : null}
        {/* Per-message point: a contribution gets this message's identity and content,
            so it can act on the message it is attached to — plus the turn and
            conversation it belongs to, which is what a backend keyed by turns needs. */}
        <ExtensionSlot
          id="app_agents_agentChat_agentChatMessage_additionalActionsButton"
          context={{
            messageId: message.id,
            role: message.role,
            text,
            taskId: message.taskId,
            createdAt: message.createdAt,
            sessionId,
          }}
        />
        {/*
          Checkpointing, on the reader's own messages and enabled on the latest of
          them. Shown disabled on the earlier ones rather than hidden, so the limit is
          where somebody looks for the feature instead of being invisible.

          A message already checkpointed keeps the button, disabled: the pill above
          says what it would do, and removing the control would make the row jump
          between two widths as the reader saves one.
        */}
        {onCheckpoint && isUser ? (
          <Tooltip
            title={
              isCheckpointed
                ? "Saved. Fork from here in the message menu."
                : canCheckpoint
                  ? "Checkpoint this message"
                  : "Only the latest message can be checkpointed."
            }
          >
            <Button
              type="text"
              size="small"
              loading={isCheckpointing}
              disabled={isCheckpointed || !canCheckpoint}
              data-testid={`chat-message-checkpoint-${message.id}`}
              aria-label={isCheckpointed ? "Checkpointed" : "Checkpoint this message"}
              aria-pressed={isCheckpointed}
              onClick={onCheckpoint}
              icon={
                <Save
                  size={14}
                  color={isCheckpointed ? theme.color.primaryText : theme.color.textMuted}
                />
              }
              css={actionButtonStyles(isCheckpointed)}
            />
          </Tooltip>
        ) : null}
        {onFork && isUser ? (
          <Dropdown
            trigger={["click"]}
            menu={{
              items: [
                {
                  key: "fork",
                  icon: <GitFork size={13} />,
                  label: "Fork chat from here",
                  disabled: !isCheckpointed,
                  title: isCheckpointed
                    ? undefined
                    : "Checkpoint this message first, then fork from it.",
                  onClick: isCheckpointed ? onFork : undefined,
                },
              ],
            }}
          >
            <Button
              type="text"
              size="small"
              data-testid={`chat-message-menu-${message.id}`}
              aria-label="Message actions"
              icon={<MoreVertical size={14} color={theme.color.textMuted} />}
              css={actionButtonStyles(false)}
            />
          </Dropdown>
        ) : null}
      </div>

      <div
        css={{
          maxWidth: "min(80ch, 100%)",
          display: "grid",
          gap: theme.space(2),
          width: isUser ? "auto" : "100%",
        }}
      >
        {message.parts.map((part, index) =>
          part.kind === "text" ? (
            part.text ? (
              <div
                key={index}
                data-testid="chat-message-text"
                css={{
                  padding: `${theme.space(2)} ${theme.space(3)}`,
                  borderRadius: theme.radius.md,
                  background: isUser ? theme.color.primary : theme.color.bgElevated,
                  border: isUser ? "none" : `1px solid ${theme.color.border}`,
                  // The user's bubble is a primary surface, so it takes the primary
                  // foreground. Using the page's `text` for both put near-black on
                  // deep purple on the light theme.
                  color: isUser ? theme.color.textOnPrimary : theme.color.text,
                  // The user's own words are shown verbatim, newlines and all. The
                  // agent's reply is markdown, which brings its own line breaks and
                  // block spacing — so `pre-wrap` is only for the user's side.
                  whiteSpace: isUser ? "pre-wrap" : undefined,
                  wordBreak: "break-word",
                }}
              >
                {isUser ? part.text : <MarkdownMessage>{part.text}</MarkdownMessage>}
              </div>
            ) : null
          ) : (
            <ToolCallCard key={index} part={part} />
          ),
        )}

        {/* A reply that has been announced but has no text yet: without this the
            message would be an invisible gap between the tool result and the
            answer, and the stream would look stalled. */}
        {isAwaitingContent(message) ? (
          <div
            data-testid="chat-message-pending"
            css={{ color: theme.color.textMuted, fontSize: 13 }}
          >
            …
          </div>
        ) : null}
      </div>
    </article>
  );
}

/**
 * Hidden until the message is hovered or the button has focus, so a transcript reads
 * as a conversation rather than a column of controls. A checkpointed message keeps
 * its button on screen: the state it reports is worth more than the quiet.
 */
function actionButtonStyles(isAlwaysShown: boolean) {
  return {
    opacity: isAlwaysShown ? 1 : 0,
    transition: "opacity 100ms ease",
    "article:hover &, &:focus-visible, &[aria-expanded='true']": { opacity: 1 },
    // antd dims a disabled text button to the point of vanishing; the checkpointed
    // one is an indicator as much as a control, so it keeps its colour.
    "&.ant-btn:disabled": { opacity: isAlwaysShown ? 1 : undefined },
  };
}
