import type { ComponentType } from "react";
import { Button, Tooltip, Typography } from "antd";
import { Bell } from "lucide-react";
import { useTheme } from "@emotion/react";
import { ExtensionSlot, useExtensionConversationPartRenderers } from "@/appExtensions";
import type { ConversationPartRendererProps, ExtensionConversationPartRenderers } from "@/appExtensions";
import type { ConversationMessage } from "@/api";
import { ToolCallCard } from "./ToolCallCard";
import { StructuredOutputCard } from "./StructuredOutputCard";
import { MarkdownMessage } from "./MarkdownMessage";
import { ToolApprovalRecord } from "./ToolApprovalRecord";
import { AskUserRecord } from "./AskUserRecord";
import { AttachmentChip } from "./AttachmentChip";
import { isAwaitingContent, messageText } from "./messageText";

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
export function ConversationMessageItem({
  message,
  conversationId,
  isCheckpointed = false,
  onManagePush,
}: {
  message: ConversationMessage;
  /** The conversation this message belongs to, for the per-message extension point. */
  conversationId?: string;
  /** Whether this message is above the nearest saved boundary, for the browser suite. */
  isCheckpointed?: boolean;
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
  const partRenderers = useExtensionConversationPartRenderers();

  return (
    <article
      data-testid="conversation-message"
      data-message-id={message.id}
      data-role={message.role}
      data-checkpointed={isCheckpointed || undefined}
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
        {/* TODO: show the message's sent time here once the backend sends one per message (#3746; UI work in 432c174). */}
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
          id="app_agents_agentConversation_agentConversationMessage_additionalActionsButton"
          context={{
            messageId: message.id,
            role: message.role,
            text,
            taskId: message.taskId,
            createdAt: message.createdAt,
            conversationId,
          }}
        />
      </div>

      <div
        data-testid="conversation-message-content"
        css={{
          maxWidth: "min(80ch, 100%)",
          // A reply holding a table or code block grows to 1120px; past that, wide tables scroll in their frame.
          "&:has([data-md-table-frame], [data-md-code])": { maxWidth: "min(1120px, 100%)" },
          display: "grid",
          gap: theme.space(2),
          width: isUser && !isQuestionRecord ? "auto" : "100%",
        }}
      >
        {message.parts.map((part, index) => {
          // Empty text is a reply still streaming in, so nothing renders it yet.
          if (part.kind === "text" && !part.text) return null;
          const key = part.kind === "data" ? part.dataKind : part.kind;
          // The key picks a renderer typed for this part; TS cannot correlate the two.
          const Renderer = (partRenderers[key] ?? CORE_PART_RENDERERS[key]) as ComponentType<
            ConversationPartRendererProps
          >;
          return (
            <Renderer
              key={index}
              part={part}
              role={message.role}
              messageId={message.id}
              taskId={message.taskId}
              conversationId={conversationId}
            />
          );
        })}

        {/* A reply that has been announced but has no text yet: without this the
            message would be an invisible gap between the tool result and the
            answer, and the stream would look stalled. */}
        {isAwaitingContent(message) ? (
          <div
            data-testid="conversation-message-pending"
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
function TextPart({ part, role }: ConversationPartRendererProps<"text">) {
  const theme = useTheme();
  const isUser = role === "user";
  return (
    <div
      data-testid="conversation-message-text"
      css={{
        // A rendered reply gets more room than a one-line user bubble.
        padding: isUser ? `${theme.space(2)} ${theme.space(3)}` : "12px 14px",
        // More room around a reply once the screen is wide enough to spare it.
        ...(isUser ? {} : { "@media (min-width: 1280px)": { padding: "20px 24px" } }),
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
function FilePart({ part, role }: ConversationPartRendererProps<"file">) {
  return (
    <div css={{ justifySelf: role === "user" ? "end" : "start", maxWidth: "100%" }}>
      <AttachmentChip file={part} />
    </div>
  );
}

/** What renders each part when no extension replaces it. */
const CORE_PART_RENDERERS: Required<ExtensionConversationPartRenderers> = {
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
