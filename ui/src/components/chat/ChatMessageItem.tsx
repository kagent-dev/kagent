import { Button, Dropdown, Typography } from "antd";
import { GitFork, MoreVertical } from "lucide-react";
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
  onFork,
}: {
  message: ChatMessage;
  /** The conversation this message belongs to, for the per-message extension point. */
  sessionId?: string;
  /**
   * Forks the conversation. Drawn only on the reader's own messages, and only when a
   * surface provides this — so a read-only view has no control that would be refused.
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
      css={{
        display: "grid",
        gap: theme.space(2),
        justifyItems: isUser ? "end" : "start",
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
        {/* The reader's own messages only. Forking reads as "rewind to this prompt and
            take a different path", so the anchor is always a question they asked — and
            a transcript of tool calls would otherwise carry a menu per row. */}
        {onFork && isUser ? (
          <Dropdown
            trigger={["click"]}
            menu={{
              items: [
                {
                  key: "fork",
                  icon: <GitFork size={13} />,
                  label: "Fork chat",
                  onClick: onFork,
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
              css={{
                // Hidden until the message is hovered or the button has focus, so a
                // transcript reads as a conversation rather than a column of controls.
                opacity: 0,
                transition: "opacity 100ms ease",
                "article:hover &, &:focus-visible, &[aria-expanded='true']": { opacity: 1 },
              }}
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
