/**
 * The live chat transport: A2A over gRPC-Web.
 *
 * This is the last call in the app to leave REST. Chat used to post JSON-RPC to
 * `/api/a2a/…` with `fetch` and parse an SSE body by hand; everything else had
 * already moved to gRPC. Now it calls `lf.a2a.v1.A2AService` on the same
 * controller, over the same transport, through the same interceptors — so there
 * is no second, quietly different path to the backend, and `src/` contains no
 * `fetch` at all.
 *
 * ## What a conversation is
 *
 * An `AgentInstance`. Not a session — there is no session id here. The gateway
 * routes on two headers rather than on a path, files every task under the
 * instance as its A2A `contextId`, and answers `ListTasks` with that
 * conversation's turns. So the instance is the address, the context and the
 * transcript at once, and `ChatConversationRef` carries the two halves the
 * headers need.
 *
 * ## Why this is so much shorter than the client it replaces
 *
 * Because protobuf-es decodes the wire. The HTTP client carried a whole section
 * of hand-written wire types and a normaliser, because the server writes its
 * union with `encoding/json` and hand-written struct tags: `result` was a keyed
 * union with no discriminator, task states arrived as `TASK_STATE_*` *names*,
 * parts carried no `kind`, and one stream used both `ROLE_AGENT` and `"user"`
 * for the role. Every one of those was a decoding decision made in this file.
 *
 * Over gRPC-Web they are the generated descriptor's job: `StreamResponse.payload`
 * is a typed oneof with a `case`, states are numeric enum members, and a `Part`
 * is a oneof rather than a shape to sniff. What is left here is the part that was
 * always this file's own — turning a stream of A2A events into the app's
 * `ChatEvent`s — and that logic is carried over unchanged, because it was right.
 *
 * ## What is deliberately kept
 *
 * The three behaviours below were each a bug fixed against a live controller, and
 * none of them is implied by the protocol:
 *
 * - **Chunks are coalesced on `adk_invocation_id`.** A streamed reply arrives as
 *   many whole messages, each with a `messageId` of its own, so delivering them
 *   as they came produced one bubble per word. What relates them is the metadata.
 * - **The final message replaces the streamed one rather than following it.**
 *   It repeats every word already shown, so emitted under a new id it printed the
 *   answer twice.
 * - **An artifact repeating text already shown is dropped.** The reply arrives
 *   both as status text and as a final artifact; an agent that sends only
 *   artifacts still works, because the check is on the text and not on the shape.
 * - **An artifact's `append` flag is honoured**, which is how this runtime actually
 *   streams. A reply comes as a run of `artifactUpdate` frames sharing one
 *   `artifactId` — one per token, each flagged `append` after the first — and then
 *   a final frame repeating the whole answer. Treated as whole messages under that
 *   shared id they replaced one another, so the transcript flickered through single
 *   tokens and printed the entire answer only at completion. Captured bytes and the
 *   frame-by-frame measurement are in the `artifactUpdate` branch below.
 */

import { create, toJson, type JsonObject } from "@bufbuild/protobuf";
import { ValueSchema } from "@bufbuild/protobuf/wkt";
import {
  A2AService,
  MessageSchema,
  Role,
  TaskState,
  type Artifact,
  type Message as A2AMessage,
  type Part as A2APart,
  type Task as A2ATask,
  type TaskStatus,
} from "@/generated/a2a_pb";
import { ApiError, fromConnectError, rethrowIfAborted } from "../ApiError";
import {
  HITL_EXTENSION_HEADER,
  HITL_EXTENSION_URI,
  readHitlRequest,
  type PendingRequest,
} from "./hitl";
import { agentInstanceShareToken } from "../shareToken";
import { interleaveTaskMessages } from "./transcriptOrder";
import { serviceClient } from "../transport";
import type {
  ChatClient,
  ChatConversationRef,
  ChatDataPart,
  ChatEvent,
  ChatHistory,
  ChatMessage,
  ChatPart,
  ChatTurnState,
  SendMessageInput,
} from "./types";

/**
 * The two headers the gateway routes on.
 *
 * `route()` in `go/core/v2/a2agateway/gateway.go` requires exactly one of each
 * and validates them — the namespace as a DNS-1123 label, the id as a UUID — so a
 * malformed pair is `InvalidRequest` rather than a call that reaches the wrong
 * agent. Sent as call metadata rather than baked into a URL because a gRPC method
 * is addressed by its descriptor: there is no path here to put them in.
 */
const NAMESPACE_HEADER = "x-kagent-agent-instance-namespace";
const INSTANCE_ID_HEADER = "x-kagent-agent-instance-id";

/** The header the controller validates a share token from. */
const SHARE_HEADER = "X-Share-Token";

/**
 * How many pages of history are followed before giving up.
 *
 * The transcript is paged — the gateway answers at most 100 tasks and hands back
 * a token — and a conversation shown with its first page only, saying nothing,
 * would be the quiet half-truth this codebase keeps having to undo. So every page
 * is followed. The cap is a backstop against a server whose token never clears,
 * which would otherwise spin here with the page stuck on a spinner.
 */
const HISTORY_PAGE_LIMIT = 50;

/*
 * Temporary bridge to A2A's ordered task timeline and artifact generation ranges:
 * https://github.com/a2aproject/A2A/pull/2129
 */
const TIMELINE_POSITION_METADATA_KEY = "kagent.dev/timeline-position";

/** Ids for the messages the wire did not name. */
let counter = 0;
function nextId(prefix: string): string {
  counter += 1;
  return `${prefix}-${counter}`;
}

/**
 * Whether a task stopped to wait on the reader rather than because it is running.
 *
 * The controller's own predicate, copied: `TaskParkedAwaitingUser` in
 * `go/api/database/client.go` is these two states and no others. Such a task is
 * non-terminal, so it holds the instance's single active-task slot and every
 * further message is refused — and the reader has to be told that rather than
 * discovering it by being turned away.
 */
function isAwaitingReply(state: TaskState | undefined): boolean {
  return state === TaskState.INPUT_REQUIRED || state === TaskState.AUTH_REQUIRED;
}

/** The turn states this client has an opinion about. */
const TURN_STATE_BY_ENUM: Partial<Record<TaskState, ChatTurnState>> = {
  [TaskState.SUBMITTED]: "submitted",
  [TaskState.WORKING]: "working",
  [TaskState.COMPLETED]: "completed",
  [TaskState.FAILED]: "failed",
  [TaskState.CANCELED]: "canceled",
  [TaskState.INPUT_REQUIRED]: "input_required",
  [TaskState.REJECTED]: "failed",
  [TaskState.AUTH_REQUIRED]: "input_required",
};

/**
 * What a task state means to the UI.
 *
 * `working` is the fallback rather than a state of its own, because every state
 * this client does not name is one where the turn is still going: an unnamed
 * terminal state would strand the composer, where an unnamed live one merely
 * keeps the spinner a moment too long. `UNSPECIFIED` is a real value on this wire
 * — the runtime sends it rather than omitting the field — and it means "the
 * runtime did not say", which is not a reason to end the turn.
 */
function turnState(state: TaskState | undefined): ChatTurnState {
  if (state === undefined) return "working";
  return TURN_STATE_BY_ENUM[state] ?? "working";
}

/** One A2A part, as something the transcript can render. */
function toPart(part: A2APart): ChatPart | undefined {
  const content = part.content;
  if (content.case === "text") {
    return { kind: "text", text: content.value };
  }
  if (content.case === "data") {
    /*
     * `Part.data` is a `google.protobuf.Value`, and a bare `Value` field is NOT
     * flattened to plain JSON the way a `Struct` field is.
     *
     * That distinction is easy to get exactly backwards. `StructuredObject.value` is
     * a `Struct`, which protobuf-es does represent as a `JsonObject` — so every
     * resource in this app is read straight off the message with no decoding. A
     * `Value` is a message with a `kind` oneof, so reading `content.value` as an
     * object yields the *wrapper*: `{kind: {case: "structValue", value: …}}`. Every
     * key a caller looks for is absent, which is why this silently classified every
     * tool call as unrecognised structured data rather than failing.
     */
    const value = toJson(ValueSchema, content.value);
    // Only an object can be a tool call or its result; a bare scalar or a list
    // carries no `name` to classify by.
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      return undefined;
    }
    const data = value as Record<string, unknown>;
    return { kind: "data", dataKind: dataKindOf(data), data };
  }
  // A file part, by url or raw bytes. Nothing renders one yet, and inventing a
  // placeholder would put a broken attachment in a transcript that has none.
  return undefined;
}

/**
 * What a data part represents.
 *
 * Read from the payload's own shape rather than from a `kind` the wire does not
 * carry: the runtime emits a tool call as `{name, args}` and its result as
 * `{name, response}`. Anything else is passed through as `unknown` and rendered
 * as structured data, which is honest — it is data, and this build does not know
 * what kind.
 */
function dataKindOf(data: Record<string, unknown>): ChatDataPart["dataKind"] {
  if ("args" in data) return "tool_call";
  if ("response" in data || "result" in data) return "tool_result";
  return "unknown";
}

function toParts(parts: readonly A2APart[] | undefined): ChatPart[] {
  const result: ChatPart[] = [];
  for (const source of parts ?? []) {
    const part = toPart(source);
    if (part === undefined) continue;

    const previous = result.at(-1);
    if (part.kind === "text" && previous?.kind === "text") {
      previous.text += part.text;
      continue;
    }
    result.push(part);
  }
  return result;
}

/** The prose of a set of parts, for comparing a reply against the artifact repeating it. */
function textOf(parts: readonly ChatPart[]): string {
  return parts
    .filter((part): part is ChatPart & { kind: "text" } => part.kind === "text")
    .map((part) => part.text)
    .join("");
}

/** When a status was recorded, as RFC3339. */
function statusTime(status: TaskStatus | undefined): string {
  const seconds = status?.timestamp?.seconds;
  if (seconds === undefined) return new Date().toISOString();
  // `Timestamp.seconds` is an int64, which protobuf-es represents as a bigint —
  // and a bigint reaching a date or a JSON payload fails in ways that read as
  // something else entirely. Narrowed here, at the boundary, as every other
  // int64 in this app is.
  return new Date(Number(seconds) * 1000).toISOString();
}

/** Whether a message is a chunk of a reply still being written. */
function isPartial(message: A2AMessage | undefined): boolean {
  return message?.metadata?.adk_partial === true;
}

/**
 * What relates the chunks of one reply.
 *
 * Every chunk is a whole message with a `messageId` of its own, so the ids cannot
 * group them; `adk_invocation_id` is the same across all of them and is the only
 * thing that can. Keyed on the invocation rather than the task, because one task
 * can hold several replies with tool calls between them.
 */
function invocationOf(message: A2AMessage | undefined, taskId: string): string {
  const invocation = message?.metadata?.adk_invocation_id;
  return typeof invocation === "string" && invocation !== "" ? invocation : taskId;
}

export class A2AGrpcChatClient implements ChatClient {
  /** What the controller's agent cards advertise. */
  readonly protocolVersion = "A2A 1.0 over gRPC-Web";

  /**
   * The call metadata addressing one conversation.
   *
   * Every RPC on this client carries it — there is no other way to say which
   * agent is meant, and a call without it is refused by the gateway rather than
   * defaulting to anything.
   */
  private callOptions(conversation: ChatConversationRef, signal?: AbortSignal) {
    const headers: Record<string, string> = {
      [NAMESPACE_HEADER]: conversation.namespace,
      [INSTANCE_ID_HEADER]: conversation.id,
      /*
       * Activate the human-in-the-loop extension, on every call.
       *
       * It is what decides whether an agent that asks the reader something parks
       * with a *payload* — questions, choices, and the correlation id an answer
       * needs — or with the question as bare prose and nothing to answer with.
       * Measured: a turn sent without this header can never be answered, and
       * re-reading it with the header does not recover what was never attached.
       * Sending it on a read costs nothing, so it is not worth being clever about
       * which calls need it.
       */
      [HITL_EXTENSION_HEADER]: HITL_EXTENSION_URI,
    };
    /*
     * A share token, when the page was opened with one for *this* conversation.
     *
     * Read here rather than applied by a request transform because these calls
     * carry no operation id — the transform interceptor passes them through
     * untouched. `shareToken.ts` holds the registration for both kinds of share so
     * there is still only one place a token is spent from.
     */
    const share = agentInstanceShareToken(conversation.namespace, conversation.id);
    if (share) headers[SHARE_HEADER] = share;
    return { signal, headers };
  }

  async history(
    conversation: ChatConversationRef,
    options: { signal?: AbortSignal } = {},
  ): Promise<ChatHistory> {
    const rpcName = "A2AService/ListTasks";
    const client = serviceClient(A2AService);
    const messages: ChatMessage[] = [];
    /*
     * The turn the conversation is parked on, if there is one.
     *
     * An instance holds at most one non-terminal task, so this cannot be
     * ambiguous — but it is taken from the last such task rather than the first,
     * so that if a cluster ever did hold two the reader is offered the one they
     * just saw rather than one from days ago.
     */
    let awaitingReply: PendingRequest | undefined;
    let pageToken = "";

    try {
      for (let page = 0; page < HISTORY_PAGE_LIMIT; page += 1) {
        const response = await client.listTasks(
          {
            // The instance's own id is its context id, so this is a belt-and-braces
            // narrowing: the gateway already scopes the read to the routed instance
            // and answers empty for any other context.
            contextId: conversation.id,
            pageToken,
            // Artifacts carry the final text of a reply, which for a completed turn
            // may be the only place it exists.
            includeArtifacts: true,
          },
          this.callOptions(conversation, options.signal),
        );

        for (const task of response.tasks) {
          messages.push(...messagesFromTask(task));
          if (isAwaitingReply(task.status?.state) && task.id) {
            // The payload is persisted with the task, so a reader who comes back
            // tomorrow gets the same choices the reader who watched it park did.
            awaitingReply =
              readHitlRequest(
                task.id,
                task.status?.message?.metadata,
                task.status?.message?.extensions,
              ) ?? { kind: "unknown", taskId: task.id };
          }
        }

        const next = response.nextPageToken;
        if (!next) return { messages, awaitingReply };
        if (next === pageToken) {
          throw new ApiError(
            "The API repeated the same page of conversation history instead of advancing.",
            { kind: "parse", url: rpcName },
          );
        }
        pageToken = next;
      }
    } catch (error) {
      rethrowIfAborted(error, options.signal);
      if (error instanceof ApiError) throw error;
      throw fromConnectError(error, rpcName);
    }

    throw new ApiError(
      `The API offered more than ${HISTORY_PAGE_LIMIT} pages of history; the conversation was not read to the end.`,
      { kind: "parse", url: rpcName },
    );
  }

  async *send(input: SendMessageInput): AsyncIterable<ChatEvent> {
    const { conversation, text, signal } = input;
    const client = serviceClient(A2AService);

    const request = {
      message: create(MessageSchema, {
        // The caller's id when it has one. It has already put this message on
        // screen under that id, and the gateway files it in the task's history
        // under whatever it is sent as — so using it is what makes the reader's
        // own words survive a reload as the *same* message rather than a second
        // one that happens to say the same thing.
        messageId: input.messageId || nextId("msg"),
        role: Role.USER,
        parts: [{ content: { case: "text" as const, value: text } }],
        // The gateway overwrites this with the instance id and refuses a value
        // that is neither empty nor the instance's own, so sending it is a
        // statement of which conversation this belongs to rather than a request.
        contextId: conversation.id,
        /*
         * An answer declares the extension on the message itself.
         *
         * Separate from the header above, and both are needed. The header activates
         * the extension for the *call*; this is what `rawHitlMap` in the runtime
         * checks before it will read the metadata at all. An answer that carries the
         * payload without this is forwarded to the agent as ordinary text — the turn
         * resumes, the agent replies, and the structured answer was never delivered.
         */
        extensions: input.hitl ? [HITL_EXTENSION_URI] : [],
        // Cast because the port states this as plain `unknown` values — it must not
        // depend on protobuf's JSON types — while the payload is JSON by
        // construction: `askUserAnswer` builds it out of strings and arrays.
        metadata: input.hitl as JsonObject | undefined,
        /*
         * The parked turn this answers, when it answers one.
         *
         * `prepareSend` accepts a task id only when it names the instance's own
         * active task *and* that task is waiting on the reader; a turn that is
         * genuinely executing still refuses one. So this is what turns a message
         * into an answer rather than a new question — and sending it for anything
         * else is refused rather than silently misfiled.
         */
        taskId: input.taskId || "",
      }),
    };

    // The reply arrives twice — once as the text of a `working` status update and
    // again as the final artifact — so an artifact repeating text already shown is
    // dropped. Recorded as the accumulated text rather than per chunk, because the
    // artifact repeats the whole answer.
    let statusReply = "";
    // The stream may echo the user's own message; when it does it must be emitted
    // only once. (This controller does not: measured on 2026-08-24, a turn carries
    // no message frame for it at all, and the caller's optimistic copy is what the
    // reader sees. The id it was sent under is what makes an echo land on that copy
    // rather than beside it.)
    const delivered = new Set<string>();

    /**
     * The text assembled so far for each artifact still being streamed.
     *
     * Keyed by `artifactId`, because that is what relates the chunks of one reply —
     * every frame of a run carries the same one. A local rather than a field: it
     * belongs to this turn and must not outlive it.
     */
    const artifacts = new Map<string, string>();

    let runId: string | undefined;
    let streamedId: string | undefined;
    /*
     * What has been shown of the reply being streamed.
     *
     * A local, not a field and not a module-level map: it belongs to this turn and
     * must not outlive it. The artifact that closes the turn repeats the whole
     * answer, so it is the accumulation that has to be recorded in `statusReply`,
     * not each chunk — recording only chunks let the artifact through and printed
     * the answer a second time.
     */
    let streamedText = "";

    let stream: AsyncIterable<{ payload: { case?: string; value?: unknown } }>;
    try {
      stream = client.sendStreamingMessage(
        request,
        this.callOptions(conversation, signal),
      );
    } catch (error) {
      rethrowIfAborted(error, signal);
      throw fromConnectError(error, "A2AService/SendStreamingMessage");
    }

    try {
      for await (const frame of stream) {
        const payload = frame.payload;

        if (payload.case === "statusUpdate") {
          const event = payload.value as {
            taskId: string;
            status?: TaskStatus;
          };
          const status = event.status;
          const message = status?.message;
          const parts = toParts(message?.parts);
          const state = turnState(status?.state);
          /*
           * The question, when this is the frame that parked the turn.
           *
           * Read from the message the status carried, which is where the runtime puts
           * it — so the choices reach the transcript as the turn parks rather than
           * waiting for the next read of history.
           */
          const awaiting =
            state === "input_required"
              ? (readHitlRequest(event.taskId, message?.metadata, message?.extensions) ?? {
                  kind: "unknown" as const,
                  taskId: event.taskId,
                })
              : undefined;

          if (message && parts.length > 0) {
            const role = message.role === Role.AGENT ? "agent" : "user";
            const isTextOnly = parts.every((part) => part.kind === "text");
            const invocation = invocationOf(message, event.taskId);
            const createdAt = statusTime(status);

            // A chunk of a reply still being written.
            if (role === "agent" && isPartial(message) && isTextOnly) {
              const chunk = textOf(parts);

              if (streamedId === undefined || runId !== invocation) {
                runId = invocation;
                streamedId = message.messageId || nextId("message");
                streamedText = chunk;
                statusReply = streamedText;
                yield {
                  type: "message",
                  message: {
                    id: streamedId,
                    role: "agent",
                    parts,
                    createdAt,
                    taskId: event.taskId,
                  },
                };
              } else if (chunk !== "") {
                streamedText += chunk;
                statusReply = streamedText;
                yield { type: "delta", messageId: streamedId, text: chunk };
              }

              yield { type: "status", state, taskId: event.taskId, awaiting };
              continue;
            }

            // The complete reply that closes a run of partials. Emitted under the
            // streamed id, which makes it a replacement rather than an addition:
            // `useChat` upserts by id, so the server's canonical text takes the place
            // of the text assembled from chunks.
            const closesRun =
              role === "agent" &&
              isTextOnly &&
              streamedId !== undefined &&
              runId === invocation;

            const id = closesRun
              ? (streamedId as string)
              : message.messageId || nextId("message");

            if (closesRun) {
              const body = textOf(parts);
              statusReply = body;
              yield {
                type: "message",
                message: { id, role: "agent", parts, createdAt, taskId: event.taskId },
              };
              yield { type: "status", state, taskId: event.taskId, awaiting };
              runId = undefined;
              streamedId = undefined;
              streamedText = "";
              continue;
            }

            // A tool call, or the user's own message: its own message, and it ends
            // any run of prose that was open.
            runId = undefined;
            streamedId = undefined;
            streamedText = "";

            if (!delivered.has(id)) {
              delivered.add(id);
              if (role === "agent") {
                statusReply = textOf(parts);
              }
              yield {
                type: "message",
                message: { id, role, parts, createdAt, taskId: event.taskId },
              };
            }
          }

          yield { type: "status", state, taskId: event.taskId, awaiting };
          continue;
        }

        if (payload.case === "artifactUpdate") {
          /*
           * The way this runtime actually streams — measured, not assumed.
           *
           * A reply arrives as a run of `artifactUpdate` frames that all carry the
           * *same* `artifactId`, one per token, and then a final frame repeating the
           * whole answer. Captured from the controller on 2026-08-24 for "Say
           * exactly: alpha beta gamma":
           *
           *   statusUpdate    WORKING
           *   artifactUpdate  id=A  "alpha"                                (no append)
           *   artifactUpdate  id=A  " beta"                                append: true
           *   artifactUpdate  id=A  " gamma"                               append: true
           *   artifactUpdate  id=A  "alpha beta gamma"                     lastChunk: true
           *   statusUpdate    COMPLETED
           *
           * Every one of those used to be emitted as a whole message under the
           * artifact's id — and because they share one id, each replaced the last.
           * The transcript flickered through single tokens and then showed the entire
           * answer at the moment the turn completed, which is precisely the reported
           * symptom: "the streaming doesn't preserve the content and only shows the
           * whole response once the message is complete."
           *
           * So `append` is honoured, exactly as the field is defined: true means add
           * this to the artifact already sent under that id, and anything else is the
           * artifact's content in full. The final frame is therefore a *replacement*
           * carrying the server's own text — the same relationship the streamed status
           * messages above already have with the message that closes their run.
           */
          const event = payload.value as {
            taskId: string;
            artifact?: Artifact;
            append?: boolean;
            lastChunk?: boolean;
          };
          const parts = toParts(event.artifact?.parts);
          if (parts.length === 0) continue;

          const body = textOf(parts);
          const artifactId = event.artifact?.artifactId || "";
          const known = artifactId !== "" && artifacts.has(artifactId);

          if (known) {
            const id = artifactId;
            if (event.append) {
              const whole = (artifacts.get(artifactId) ?? "") + body;
              artifacts.set(artifactId, whole);
              yield { type: "delta", messageId: id, text: body };
            } else {
              artifacts.set(artifactId, body);
              yield {
                type: "message",
                message: {
                  id,
                  role: "agent",
                  parts,
                  createdAt: new Date().toISOString(),
                  taskId: event.taskId,
                },
              };
            }
            continue;
          }

          // First sight of this artifact. Dropped when it merely repeats prose the
          // reader has already been shown as status text — the same reply arriving
          // twice, which is the behaviour this branch was originally written for.
          if (body !== "" && statusReply === body) {
            statusReply = "";
            continue;
          }
          // Text equality is only a compatibility check between adjacent wire
          // representations of one reply. It is not artifact identity and must
          // not survive across a real artifact or tool boundary.
          statusReply = "";

          const id = artifactId || nextId("artifact");
          artifacts.set(id, body);
          yield {
            type: "message",
            message: {
              id,
              role: "agent",
              parts,
              createdAt: new Date().toISOString(),
              taskId: event.taskId,
            },
          };
          continue;
        }

        // A non-streaming agent answers with the whole task instead of updates.
        if (payload.case === "task") {
          const task = payload.value as A2ATask;
          for (const message of messagesFromTask(task)) {
            if (delivered.has(message.id)) continue;
            delivered.add(message.id);
            yield { type: "message", message };
          }
          yield { type: "status", state: turnState(task.status?.state), taskId: task.id };
          continue;
        }

        if (payload.case === "message") {
          const message = payload.value as A2AMessage;
          const parts = toParts(message.parts);
          if (parts.length === 0) continue;
          const id = message.messageId || nextId("message");
          if (delivered.has(id)) continue;
          delivered.add(id);
          yield {
            type: "message",
            message: {
              id,
              role: message.role === Role.AGENT ? "agent" : "user",
              parts,
              createdAt: new Date().toISOString(),
              taskId: message.taskId || undefined,
            },
          };
        }
      }
    } catch (error) {
      // A caller-driven abort is the user pressing stop, not a failure.
      rethrowIfAborted(error, signal);
      if (signal?.aborted) return;
      throw fromConnectError(error, "A2AService/SendStreamingMessage");
    }
  }

  async cancel(conversation: ChatConversationRef, taskId: string): Promise<void> {
    try {
      await serviceClient(A2AService).cancelTask(
        { id: taskId },
        this.callOptions(conversation),
      );
    } catch (error) {
      throw fromConnectError(error, "A2AService/CancelTask");
    }
  }
}

/**
 * The messages of a task, for replaying a conversation.
 *
 * The gateway repeats the first user message in `history`, so a repeat of a
 * message already taken is skipped — without that, every reopened conversation
 * shows its opening line twice.
 *
 * Identity is the message id, or the parts themselves when there is none, and
 * deliberately not the text: a tool call and its result are consecutive agent
 * messages carrying data parts and no text at all, so comparing text made them
 * look identical and dropped the result — a replayed conversation showed every
 * finished tool call still waiting to return.
 *
 * ## When these messages happened
 *
 * The task's own status timestamp, not the clock now. History messages carry no
 * time of their own, so stamping them with the moment the conversation was
 * reopened reads as true — a conversation replays in order and nothing on screen
 * shows the time — and is wrong by however long ago the conversation was.
 */
export function messagesFromTask(task: A2ATask): ChatMessage[] {
  const messages: ChatMessage[] = [];
  const positioned: { message: ChatMessage; position?: string }[] = [];
  const taken = new Set<string>();
  const createdAt = statusTime(task.status);
  const hasCompleteTimeline = [...task.history, ...(task.status?.message ? [task.status.message] : []), ...task.artifacts]
    .every((entry) => timelinePosition(entry.metadata) !== undefined);

  const push = (message: A2AMessage) => {
    const parts = toParts(message.parts);
    if (parts.length === 0) return;
    const identity =
      message.messageId || JSON.stringify(message.parts.map((part) => part.content));
    if (taken.has(identity)) return;
    taken.add(identity);
    const converted: ChatMessage = {
      /*
       * Derived from the task and the position in it, never from a counter.
       *
       * The gateway does not name an agent reply, so this is the branch most replies
       * take — and it used to take a process-wide counter, which meant the same reply
       * came back as `history-1` on one read and `history-2` on the next. The
       * transcript merge treats the id as identity, so on every re-read the copy
       * already on screen stopped matching and was kept as a local extra, appended
       * after everything the server sent: a reply from three turns ago reappearing
       * under the newest one, and a refresh — which drops the local copy and the
       * counter together — putting it right. Focus was enough to trigger it.
       *
       * Position within the task is stable for the same payload and unaffected by the
       * task gaining later messages, which is all the merge needs.
       */
      id: message.messageId || `${task.id || "task"}-message-${messages.length}`,
      role: message.role === Role.AGENT ? "agent" : "user",
      parts,
      createdAt,
      taskId: task.id || undefined,
    };
    messages.push(converted);
    positioned.push({ message: converted, position: timelinePosition(message.metadata) });
  };

  /*
   * Sorted into three, because the gateway hands back two lists with no way to
   * interleave them — see `interleaveTaskMessages`, which does the inferring and
   * carries the reasoning.
   *
   * The split has to happen here rather than after conversion: what marks a reader
   * turn as an answer is the HITL metadata on the A2A message, and a `ChatMessage`
   * does not carry it.
   */
  const openingCount = messages.length;
  const answerAt: number[] = [];
  for (const message of task.history) {
    if (isAskUserResponse(message)) answerAt.push(messages.length);
    push(message);
  }
  if (task.status?.message) push(task.status.message);

  const fromHistory = messages.slice(openingCount);
  const answered = new Set(answerAt.map((index) => index - openingCount));
  const answers = fromHistory.filter((_, index) => answered.has(index));
  const opening = [
    ...messages.slice(0, openingCount),
    ...fromHistory.filter((_, index) => !answered.has(index)),
  ];

  // An artifact repeating text already in the history is the same reply arriving
  // twice, exactly as it is on a live stream.
  const shown = new Set(messages.map((message) => textOf(message.parts)));
  const agent: ChatMessage[] = [];
  for (const artifact of task.artifacts) {
    const parts = toParts(artifact.parts);
    const body = textOf(parts);
    if (
      parts.length === 0 ||
      (!hasCompleteTimeline && body !== "" && shown.has(body))
    ) continue;
    const converted: ChatMessage = {
      // Derived, for the reason given against the message id above: an unnamed
      // artifact renamed on every read is an artifact the merge cannot recognise.
      id: artifact.artifactId || `${task.id || "task"}-artifact-${messages.length + agent.length}`,
      role: "agent",
      parts,
      createdAt,
      taskId: task.id || undefined,
    };
    agent.push(converted);
    positioned.push({ message: converted, position: timelinePosition(artifact.metadata) });
  }

  // New records carry one server-authored order across history and artifacts.
  // Keep the positional inference below for records written before this bridge.
  if (positioned.length > 0 && positioned.every(({ position }) => position !== undefined)) {
    return positioned
      .sort((left, right) => left.position!.localeCompare(right.position!))
      .map(({ message }) => message);
  }

  return interleaveTaskMessages(opening, answers, agent);
}

function timelinePosition(metadata: JsonObject | undefined): string | undefined {
  const value = metadata?.[TIMELINE_POSITION_METADATA_KEY];
  return typeof value === "string" ? value : undefined;
}

/** Whether a reader's turn is answering an `ask_user` rather than opening a task. */
function isAskUserResponse(message: A2AMessage): boolean {
  const carried = (message.metadata as Record<string, unknown> | undefined)?.[
    HITL_EXTENSION_URI
  ] as { type?: unknown } | undefined;
  return carried?.type === "ask_user_response";
}
