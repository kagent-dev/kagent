/**
 * The chat port: what the UI needs from a conversation transport, stated
 * without reference to how that transport works.
 *
 * kagent speaks A2A, and A2A is moving to 1.0 — a change of wire types, of
 * streaming envelope, and of client library. Everything the chat UI renders is
 * defined here instead, in the app's own vocabulary, so that migration replaces
 * one implementation of `ChatClient` and touches nothing that renders.
 */

import type { AskUserRecord, PendingRequest, ToolApprovalRecord } from "./hitl";

export type ChatRole = "user" | "agent";

export interface ChatTextPart {
  kind: "text";
  text: string;
}

/** Structured content: a tool call, its result, or anything else non-prose. */
export interface ChatDataPart {
  kind: "data";
  /** What the payload represents, so a renderer can pick a component. */
  dataKind: "tool_call" | "tool_result" | "tool_not_run" | "unknown";
  data: Record<string, unknown>;
}

/** A structured human decision, rendered as an audit-friendly status card. */
export interface ChatToolApprovalPart {
  kind: "tool_approval";
  approval: ToolApprovalRecord;
}

/** A completed ask-user exchange, rendered without its protocol artifacts. */
export interface ChatAskUserPart {
  kind: "ask_user";
  interaction: AskUserRecord;
}

export type ChatPart = ChatTextPart | ChatDataPart | ChatToolApprovalPart | ChatAskUserPart;

export interface ChatMessage {
  id: string;
  role: ChatRole;
  parts: ChatPart[];
  /** RFC3339. */
  createdAt: string;
  /** The turn this message belongs to, when the transport groups turns. */
  taskId?: string;
}

/** Lifecycle of one agent turn. */
export type ChatTurnState =
  | "submitted"
  | "working"
  | "input_required"
  | "completed"
  | "failed"
  | "canceled";

export type ChatEvent =
  /** A complete message arrived. */
  | { type: "message"; message: ChatMessage }
  /** More text for a message already delivered — the streaming case. */
  | { type: "delta"; messageId: string; text: string }
  /**
   * The turn changed state.
   *
   * `awaiting` rides along when the turn stopped to ask the reader something, because
   * that is where the payload is on the wire — the question, its choices and the
   * correlation id are the metadata of the very message the status carried. Reported
   * here rather than left to a re-read, so the choices appear as the turn parks
   * rather than only after a reload.
   */
  | { type: "status"; state: ChatTurnState; taskId?: string; awaiting?: PendingRequest }
  /** The turn failed. The stream ends after this. */
  | { type: "error"; error: Error };

/**
 * Which conversation, which is to say which agent.
 *
 * An `AgentInstance` *is* the conversation. There is no separate session id: the
 * A2A gateway files every task under the instance as its `contextId`, and
 * `ListTasks` for the instance is the transcript. So a conversation is addressed
 * the way an instance is — `(namespace, id)` — and both halves are needed on every
 * call, because the gateway routes on two headers rather than on a path.
 */
export interface ChatConversationRef {
  namespace: string;
  /** The AgentInstance id. A UUID; the gateway rejects anything else. */
  id: string;
}

/** How a conversation is keyed in a map or a React dependency list. */
export function conversationKey(conversation: ChatConversationRef): string {
  return `${conversation.namespace}/${conversation.id}`;
}

export interface SendMessageInput {
  conversation: ChatConversationRef;
  text: string;
  /**
   * The id the caller has already filed this message under.
   *
   * A2A lets the *client* name the message it is sending, and this is why that
   * matters here: the UI puts the reader's words on screen the moment they press
   * send, rather than waiting to be told they exist. Whatever comes back for that
   * message — the gateway echoing it, or a later read of the conversation's
   * history — then carries the same id and replaces what is already there instead
   * of arriving beside it as a second copy.
   *
   * Optional, so a caller with no transcript to keep in step can leave the
   * transport to invent one.
   */
  messageId?: string;
  /**
   * The parked turn this message answers, when it answers one.
   *
   * A turn that called a tool to ask the reader something stops in
   * `input_required` and stays the instance's active task. A message naming it
   * *resumes* that turn rather than opening a second one — which is why answering
   * does not violate the one-turn-per-instance rule the controller enforces, and
   * why the id has to travel: without it the gateway mints a fresh task and the
   * question goes unanswered while the send looks like it worked.
   */
  taskId?: string;
  /**
   * The structured answer to the question that turn asked.
   *
   * Built by `askUserAnswer` in `hitl.ts`, and carried as the message's own
   * metadata. When it is present the message also declares the extension, which the
   * runtime requires before it will read the metadata at all — an answer without
   * that declaration is forwarded as ordinary text and the agent answers nothing.
   */
  hitl?: Record<string, unknown>;
  /** Aborts the turn; the stream ends without an error event. */
  signal?: AbortSignal;
}

/**
 * A conversation as it stands, which is more than the messages in it.
 *
 * A conversation can be *holding a question*: the agent called a tool that asks the
 * reader something, and its turn parked in `input_required` rather than finishing.
 * That turn is non-terminal, so it keeps the instance's single active-task slot, and
 * the controller refuses any further message until it is answered or given up —
 * `FailedPrecondition`, "the agent is waiting for a reply to its last message".
 *
 * The messages alone cannot say that. The question renders as ordinary agent prose,
 * so a reopened conversation holding one looks finished, and the next thing the
 * reader does is refused for a reason nothing on screen explains. That was reported
 * as an agent that "suddenly stopped working". So the state travels with the
 * transcript.
 */
export interface ChatHistory {
  messages: ChatMessage[];
  /**
   * The request the parked turn is holding, when the conversation is holding one.
   *
   * Carries the task id because everything the reader can do about it needs one:
   * answering means sending a message that names this turn, and giving up means
   * cancelling it. What it is *asking* rides along too, when the turn was asked
   * through the HITL extension — see `hitl.ts` for why that is not always.
   */
  awaitingReply?: PendingRequest;
}

export interface ChatClient {
  /**
   * Which transport this is, for display and for support questions.
   *
   * Implementations must treat every `ChatMessage` they emit as immutable: a
   * consumer stores what it is handed, so mutating a message after yielding it
   * applies the change twice — once through the shared reference and once
   * through the event that reports it.
   */
  readonly protocolVersion: string;
  /** Everything said in this conversation so far, oldest first, and its state. */
  history(
    conversation: ChatConversationRef,
    options?: { signal?: AbortSignal },
  ): Promise<ChatHistory>;
  /** Sends a message and streams the agent's turn back as it happens. */
  send(input: SendMessageInput): AsyncIterable<ChatEvent>;
  /** Asks the agent to stop the named turn. */
  cancel(conversation: ChatConversationRef, taskId: string): Promise<void>;
}
