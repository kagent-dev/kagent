import type { ComponentType } from "react";
import { Button, Tooltip, Typography } from "antd";
import { Bell } from "lucide-react";
import { useTheme } from "@emotion/react";
import { ExtensionSlot, useExtensionChatPartRenderers } from "@/appExtensions";
import type { ChatPartRendererProps, ExtensionChatPartRenderers } from "@/appExtensions";
import type { ChatMessage } from "@/api";
import { ToolCallCard } from "./ToolCallCard";
import { StructuredOutputCard } from "./StructuredOutputCard";
import { MarkdownMessage } from "./MarkdownMessage";
import { ToolApprovalRecord } from "./ToolApprovalRecord";
import { AskUserRecord } from "./AskUserRecord";
import { AttachmentChip } from "./AttachmentChip";
import { isAwaitingContent, messageText } from "./messageText";
import type { DiscardedMark } from "./discardedTurns";

const { Text } = Typography;

/**
 * One message: prose from the user or the agent, or a tool call and its result.
 *
 * A message can hold several parts, so this renders each part in order rather
 * than picking one shape per message — a turn that calls a tool and then
 * explains itself is one message in the transport's terms.
 *
 * A saved boundary is not drawn here: it falls *between* messages, so the transcript
 * draws `CheckpointDivider` after the turn it was taken at.
 */
export function ChatMessageItem({
  message,
  sessionId,
  isCheckpointed = false,
  discarded,
  onManagePush,
}: {
  message: ChatMessage;
  /** The conversation this message belongs to, for the per-message extension point. */
  sessionId?: string;
  /** Whether this message is above the nearest saved boundary, for the browser suite. */
  isCheckpointed?: boolean;
  /** Whether a runtime recovery discarded this message, and how; see `discardedMessages`. */
  discarded?: DiscardedMark;
  /** Opens the callback settings for this message's task. */
  onManagePush?: () => void;
}) {
  const theme = useTheme();
  const isUser = message.role === "user";
  // A completed question is a transcript summary and keeps the full notification
  // lane. Tool approval decisions are direct user responses, so they deliberately
  // retain the ordinary right-aligned, content-sized user lane.
  const isQuestionRecord = message.parts.some((part) => part.kind === "ask_user");
  const text = messageText(message);
  const partRenderers = useExtensionChatPartRenderers();

  return (
    <article
      data-testid="chat-message"
      data-message-id={message.id}
      data-role={message.role}
      data-checkpointed={isCheckpointed || undefined}
      data-discarded={discarded}
      css={{
        display: "grid",
        gap: theme.space(2),
        justifyItems: isUser && !isQuestionRecord ? "end" : "start",
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
        {/* Labelled in the header rather than only dimmed: the reader message above it
            carries the explanation, and a dimmed reply on its own reads as styling. */}
        {discarded === "reply" ? (
          <Text css={{ color: "inherit", fontSize: "inherit" }}>
            · Not remembered after recovery
          </Text>
        ) : null}
        {onManagePush ? (
          <Tooltip title="Task notifications">
            <Button
              type="text"
              size="small"
              icon={<Bell size={14} aria-hidden />}
              aria-label="Task notifications"
              data-testid={`task-push-open-${message.taskId}`}
              onClick={onManagePush}
            />
          </Tooltip>
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
      </div>

      <div
        data-testid="chat-message-content"
        css={{
          maxWidth: "min(80ch, 100%)",
          display: "grid",
          gap: theme.space(2),
          width: isUser && !isQuestionRecord ? "auto" : "100%",
          // Still readable, because it is still what was said; only no longer in the
          // runtime's memory.
          opacity: discarded ? 0.6 : undefined,
        }}
      >
        {message.parts.map((part, index) => {
          // Empty text is a reply still streaming in, so nothing renders it yet.
          if (part.kind === "text" && !part.text) return null;
          const key = part.kind === "data" ? part.dataKind : part.kind;
          // The key picks a renderer typed for this part; TS cannot correlate the two.
          const Renderer = (partRenderers[key] ?? CORE_PART_RENDERERS[key]) as ComponentType<
            ChatPartRendererProps
          >;
          return (
            <Renderer
              key={index}
              part={part}
              role={message.role}
              messageId={message.id}
              taskId={message.taskId}
              sessionId={sessionId}
            />
          );
        })}

        {/* Not "failed to send": the message reached the agent, which may have answered
            part of it before the crash. Not a live region either: this is persisted
            history, and a status role would announce every note on each load. */}
        {discarded === "message" ? (
          <Text type="warning">
            The runtime crashed during this turn. After recovery, the agent does not
            remember this message or the replies after it.
          </Text>
        ) : null}

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

/** The prose bubble: the user's words verbatim, the agent's reply as markdown. */
function TextPart({ part, role }: ChatPartRendererProps<"text">) {
  const theme = useTheme();
  const isUser = role === "user";
  return (
    <div
      data-testid="chat-message-text"
      css={{
        padding: `${theme.space(2)} ${theme.space(3)}`,
        borderRadius: theme.radius.md,
        background: isUser ? theme.color.primary : theme.color.bgElevated,
        border: isUser ? "none" : `1px solid ${theme.color.border}`,
        // A primary surface takes the primary foreground; the page's text was unreadable on it.
        color: isUser ? theme.color.textOnPrimary : theme.color.text,
        // Only the user's words need pre-wrap; the agent's markdown brings its own breaks.
        whiteSpace: isUser ? "pre-wrap" : undefined,
        wordBreak: "break-word",
      }}
    >
      {isUser ? part.text : <MarkdownMessage>{part.text}</MarkdownMessage>}
    </div>
  );
}

/** A sent or received file, as a download chip on the message's side. */
function FilePart({ part, role }: ChatPartRendererProps<"file">) {
  return (
    <div css={{ justifySelf: role === "user" ? "end" : "start", maxWidth: "100%" }}>
      <AttachmentChip file={part} />
    </div>
  );
}

/** What renders each part when no extension replaces it. */
const CORE_PART_RENDERERS: Required<ExtensionChatPartRenderers> = {
  text: TextPart,
  file: FilePart,
  tool_call: ToolCallCard,
  tool_result: ToolCallCard,
  tool_not_run: ToolCallCard,
  unknown: ToolCallCard,
  structured_output: StructuredOutputCard,
  tool_approval: ToolApprovalRecord,
  ask_user: AskUserRecord,
};
