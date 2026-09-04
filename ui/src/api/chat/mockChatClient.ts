/**
 * A conversation transport with no backend behind it.
 *
 * It replays a scripted turn — status, a tool call, its result, streamed text —
 * so the chat UI can be built and demonstrated against every rendering case
 * before the real A2A client exists, and so those cases stay reachable
 * afterwards without a cluster.
 *
 * This is mock infrastructure that happens to live beside the port it
 * implements, which is why it reads the chat scenario from `src/mocks`: the
 * alternative is a second copy of that contract, and two copies drift. The real
 * client will never import any of it.
 */

import { currentChatScenario } from "@/mocks/scenario";
import { allAgentInstances, instanceShareForToken } from "@/mocks/state";
import { ApiError } from "../ApiError";
import { agentInstanceShareToken } from "../shareToken";
import { HITL_EXTENSION_URI, type PendingRequest } from "./hitl";
import { conversationKey } from "./types";
import type {
  ChatClient,
  ChatConversationRef,
  ChatEvent,
  ChatHistory,
  ChatMessage,
  SendMessageInput,
} from "./types";

const TIMING = {
  ok: { step: 300, word: 45 },
  // Slow enough that a turn can be reliably cancelled mid-stream, which is the
  // only way to test cancelling at all.
  slow: { step: 1_200, word: 400 },
  error: { step: 300, word: 45 },
  asks: { step: 300, word: 45 },
  "asks-text": { step: 300, word: 45 },
} as const;

/**
 * What the scripted `ask_user` turn asks.
 *
 * Two questions, one single-choice and one multi, because the payload's `multiple`
 * flag decides how each renders and how many answers go back — and a fixture with
 * one question of one kind leaves the other half of that untested.
 */
const QUESTION = "Which pizza toppings would you like? You can choose more than one.";
const TOPPINGS = ["Pepperoni", "Mushroom", "Pineapple"];
const SIZE_QUESTION = "What size pizza would you like?";
const SIZES = ["Small", "Medium", "Large"];
/** The one-field variant: no choices offered, so the agent wants prose. */
const NOTE_QUESTION = "What should I put on the order note?";
/** The correlation id, which a real answer echoes verbatim. */
const REQUEST_ID = "adk-mock-ask-1";

/** Where the scripted turn gives up when the scenario asks it to fail. */
const FAILURE_MESSAGE =
  "The agent stopped responding. The connection to the runtime was lost.";

/**
 * Where this fixture says what it is doing, for the browser suite to wait on — the
 * same instrument `src/mocks/transport.ts` publishes for RPC calls, and for the same
 * reason: under a substituted transport there is no request on the wire to watch.
 * Counters rather than a boolean, so a waiter can name the turn it means.
 */
export const MOCK_CHAT_PROPERTY = "__kagentMockChat";

export interface MockChatActivity {
  /** Turns this fixture has begun streaming since the page loaded. */
  started: number;
  /** Turns it has stopped streaming — completed, failed, parked, or cancelled. */
  finished: number;
}

const activity: MockChatActivity = { started: 0, finished: 0 };

/**
 * Called from the constructor rather than at module load: this module is imported in
 * every build and instantiated only in mock mode.
 */
function publishActivity(): void {
  if (typeof window === "undefined") return;
  (window as unknown as Record<string, unknown>)[MOCK_CHAT_PROPERTY] = activity;
}

export class MockChatClient implements ChatClient {
  readonly protocolVersion = "mock";

  /** Per-conversation transcript, so history reflects what was actually said. */
  private readonly transcripts = new Map<string, ChatMessage[]>();
  private turn = 0;

  /**
   * Distinguishes this client's turns from those of any earlier one.
   *
   * The turn counter restarts with each new client, but transcripts now outlive
   * it — so without this, the first turn after a reload reuses the message ids
   * of the first turn before it, and React sees two children with the same key.
   */
  private readonly runId = Math.random().toString(36).slice(2, 10);

  constructor() {
    publishActivity();
  }

  async history(
    conversation: ChatConversationRef,
    options: { signal?: AbortSignal } = {},
  ): Promise<ChatHistory> {
    await delay(250, options.signal);
    refuseAnInvalidShare(conversation);
    const sessionId = conversationKey(conversation);
    // An instance that was talked to before opens with that conversation in it; a
    // brand-new one opens empty. Both are states the UI has to render.
    //
    // Snapshots, not the stored objects: a consumer keeps what it is handed, and
    // this client goes on mutating its own copies as turns stream.
    return {
      messages: this.transcriptFor(sessionId).map(snapshot),
      // A parked turn outlives the page that watched it park, exactly as it does on
      // a cluster: the controller keeps the task non-terminal, so a reload lands
      // back in a conversation that is still holding a question.
      awaitingReply: loadParked(sessionId),
    };
  }

  /**
   * One turn, counted at both ends. Kept out of the script below because `finally` is
   * the one place that sees every ending — completed, refused, failed, parked, or
   * cancelled by a consumer that stopped iterating.
   */
  async *send(input: SendMessageInput): AsyncIterable<ChatEvent> {
    activity.started += 1;
    try {
      yield* this.stream(input);
    } finally {
      activity.finished += 1;
    }
  }

  private async *stream(input: SendMessageInput): AsyncIterable<ChatEvent> {
    const { conversation, text, signal } = input;
    const sessionId = conversationKey(conversation);
    const scenario = currentChatScenario();
    const timing = TIMING[scenario];

    /*
     * The controller refuses a message while a question is pending, and so does
     * this — in the controller's own words, from `storeError` in
     * `go/core/v2/a2agateway/gateway.go`. A fixture that accepted it would let a
     * build that never shows the parked state pass every browser test, which is
     * how the reported conversation came to look like an agent that had broken.
     */
    /*
     * The gateway refuses a message for an instance that is not ready, and so does
     * this. It answers `UnsupportedOperation` naming the state — see the precondition
     * on `SendStreamingMessage` in `go/core/v2/a2agateway/gateway.go`.
     *
     * Added because its absence made a real property untestable rather than merely
     * untested: a page that sends into a suspended conversation without resuming it
     * first gets a refusal on a cluster and a perfectly good answer here, so every
     * browser test passed either way. Both products resume before sending; without
     * this, nothing checks that they do.
     */
    const instance = allAgentInstances().find(
      (row) =>
        row.namespace === conversation.namespace && row.id === conversation.id,
    );
    if (instance && instance.state !== "ready") {
      yield {
        type: "error",
        error: new ApiError(
          `agent instance is ${instance.state}, not ready; resume it before sending`,
          { kind: "http", status: 400, url: "A2AService/SendStreamingMessage" },
        ),
      };
      return;
    }

    const parked = loadParked(sessionId);
    if (parked && input.taskId !== parked.taskId) {
      yield {
        type: "error",
        error: new ApiError(
          "the agent is waiting for a reply to its last message; answer it, or cancel that task to start a new one",
          { kind: "http", status: 400, url: "A2AService/SendStreamingMessage" },
        ),
      };
      return;
    }

    if (parked) {
      /*
       * The answer resumes the parked turn rather than opening a new one.
       *
       * That is the gateway's own rule: a parked task already *is* the instance's
       * active-task slot, so a message naming it continues that turn and the
       * one-turn-per-instance invariant holds untouched. A message that does not
       * name it is refused above, in the controller's own words.
      */
      const answered = this.transcriptFor(sessionId);
      const structured = readAnswer(input.hitl);
      const reply: ChatMessage = {
        id: input.messageId ?? `${parked.taskId}-answer`,
        role: "user",
        parts:
          structured && parked.kind === "ask_user" && structured.id === parked.requestId
            ? [
                {
                  kind: "ask_user",
                  interaction: {
                    questions: parked.questions,
                    answers: structured.answers,
                    askedBy: parked.askedBy,
                  },
                },
              ]
            : [{ kind: "text", text }],
        createdAt: new Date().toISOString(),
        taskId: parked.taskId,
      };
      answered.push(reply);
      /*
       * What the agent understood, which is not the same as what it received.
       *
       * The runtime reads the structured answer only from a message that both
       * declares the extension and carries the payload under its URI; anything else
       * reaches the agent as ordinary prose, the turn resumes, and the reply reads
       * as though it worked. So the acknowledgement here says which happened — that
       * silent failure is the reason this fixture bothers to check.
       */
      const acknowledgement = message(
        `${parked.taskId}-ack`,
        "agent",
        structured && parked.kind === "ask_user" && structured.id === parked.requestId
          ? `Noted: **${structured.answers.map((a) => a.join(", ")).join("; ")}**.`
          : `I did not catch a choice in that.`,
        parked.taskId,
      );
      answered.push(acknowledgement);
      this.persist(sessionId);
      clearParked(sessionId);

      yield { type: "status", state: "working", taskId: parked.taskId };
      if (await stopped(signal, timing.step)) return;
      yield { type: "message", message: snapshot(acknowledgement) };
      yield { type: "status", state: "completed", taskId: parked.taskId };
      return;
    }

    this.turn += 1;
    const taskId = `mock-${this.runId}-task-${this.turn}`;
    const transcript = this.transcriptFor(sessionId);

    /*
     * The reader's own message is *recorded* and never *announced*.
     *
     * That asymmetry is the whole point, and it is copied from the cluster rather
     * than chosen. Measured against the A2A gateway on 2026-08-24: a live turn
     * emits two frames — `WORKING` then `COMPLETED` — and no message frame of any
     * kind, while `ListTasks` afterwards holds the user's message and nothing
     * else. So a backend remembers what the reader said and never says it back.
     *
     * This client used to yield it as an event, and that single line of
     * generosity kept the whole browser suite green over the defect it existed to
     * catch: the transcript looked right in fixtures and was empty of the reader's
     * words on a cluster, until they reloaded. A fixture more permissive than the
     * backend is a fixture that hides the bug it is for.
     */
    const userMessage = message(input.messageId ?? `${taskId}-user`, "user", text, taskId);
    transcript.push(userMessage);
    this.persist(sessionId);
    yield { type: "status", state: "submitted", taskId };

    if (await stopped(signal, timing.step)) return;
    yield { type: "status", state: "working", taskId };

    if (scenario === "error") {
      // Fail after the turn is visibly under way, which is the case worth
      // rendering: the user has already seen it start.
      if (await stopped(signal, timing.step)) return;
      yield { type: "error", error: new Error(FAILURE_MESSAGE) };
      return;
    }

    const toolCall = dataMessage(`${taskId}-tool-call`, taskId, "tool_call", {
      id: `call-${taskId}`,
      name: "k8s_get_pods",
      args: { namespace: "kagent" },
    });
    transcript.push(toolCall);
    this.persist(sessionId);
    yield { type: "message", message: snapshot(toolCall) };

    if (await stopped(signal, timing.step)) return;

    const toolResult = dataMessage(`${taskId}-tool-result`, taskId, "tool_result", {
      id: `call-${taskId}`,
      name: "k8s_get_pods",
      response: { output: "3 pods running in kagent" },
    });
    transcript.push(toolResult);
    this.persist(sessionId);
    yield { type: "message", message: snapshot(toolResult) };

    if (await stopped(signal, timing.step)) return;

    // Streamed prose: an empty message first, then deltas — mirroring how a real
    // transport delivers a response the UI has to append to rather than replace.
    //
    // The announcement is a snapshot, not the transcript's own object: this
    // method goes on to mutate `reply` as chunks arrive, and handing the
    // consumer that same reference would apply the first chunk twice — once
    // through the shared object, once through the delta event.
    const reply = message(`${taskId}-reply`, "agent", "", taskId);
    transcript.push(reply);
    this.persist(sessionId);
    yield { type: "message", message: snapshot(reply) };

    // Markdown, because a real agent writes it and this page renders it — a plain-prose
    // fixture would leave the whole `MarkdownMessage` path unexercised by every browser
    // test, which is how it could break without anything failing.
    const answer = [
      "There are **3 pods** running in the `kagent` namespace:",
      "",
      "- `kagent-controller` — the reconciler",
      "- `kagent-ui` — this page",
      "- `kagent-tools` — the tool server",
      "",
      `You asked: "${text}".`,
    ].join("\n");
    for (const chunk of answer.match(/\S+\s*/g) ?? []) {
      if (await stopped(signal, timing.word)) return;
      appendText(reply, chunk);
      yield { type: "delta", messageId: reply.id, text: chunk };
    }

    // Committed once the reply is whole rather than per chunk: a partial answer
    // is not something a backend would have recorded as the turn's result.
    this.persist(sessionId);

    if (scenario === "asks" || scenario === "asks-text") {
      /*
       * The turn ends by asking rather than by finishing.
       *
       * The real transport receives a raw call and fallback prose here, then
       * normalises both away. This mock implements the same ChatClient boundary,
       * so the structured pending request below is the only representation.
       */
      const questions =
        scenario === "asks-text"
          ? [{ question: NOTE_QUESTION, choices: [], multiple: false }]
          : [
              { question: SIZE_QUESTION, choices: SIZES, multiple: false },
              { question: QUESTION, choices: TOPPINGS, multiple: true },
            ];

      this.persist(sessionId);
      const request: PendingRequest = {
        kind: "ask_user",
        taskId,
        requestId: REQUEST_ID,
        questions,
      };
      saveParked(sessionId, request);
      // The payload rides on the status, as it does on the wire — the choices are
      // the metadata of the message this status carried.
      yield { type: "status", state: "input_required", taskId, awaiting: request };
      return;
    }

    yield { type: "status", state: "completed", taskId };
  }

  /**
   * Acknowledges the stop. Nothing is added to the transcript: the turn is
   * already over locally, and a note appended here would be invisible until the
   * next time history was fetched — a message that materialises on reload and
   * was never in the conversation the user watched. The UI reports a cancelled
   * turn from its own state instead.
   */
  async cancel(conversation: ChatConversationRef, taskId: string): Promise<void> {
    await delay(120);
    /*
     * Giving up the question frees the conversation, which is the controller's own
     * behaviour and the only way out of a parked turn: `CancelTask` records the
     * cancellation locally even when the runtime cannot help, precisely so a
     * conversation holding a question can always be recovered.
     */
    const sessionId = conversationKey(conversation);
    if (loadParked(sessionId)?.taskId === taskId) clearParked(sessionId);
  }

  private transcriptFor(sessionId: string): ChatMessage[] {
    const existing = this.transcripts.get(sessionId);
    if (existing) return existing;
    const fresh = loadTranscript(sessionId) ?? SEEDED_TRANSCRIPTS[sessionId]?.() ?? [];
    this.transcripts.set(sessionId, fresh);
    return fresh;
  }

  /** Records the conversation so far, the way a real backend would have. */
  private persist(sessionId: string): void {
    saveTranscript(sessionId, this.transcripts.get(sessionId) ?? []);
  }
}

/**
 * Where a conversation lives between page loads.
 *
 * `sessionStorage`, not memory alone: a real backend remembers what was said, so
 * a mock that forgets on reload would make the UI look broken in a way the
 * product is not. Scoped to the tab, so it starts clean for every browser
 * context — which is what keeps one test's conversation out of the next one's.
 */
const STORAGE_PREFIX = "kagent.mockChat.";

/**
 * Where a pending question lives between page loads.
 *
 * Beside the transcript and not inside it, because a pending interaction is task
 * state rather than a message. The raw protocol call and prose do not render; what
 * has to survive is the request and the fact that the *turn* never ended.
 */
const PARKED_PREFIX = "kagent.mockChat.parked.";

function loadParked(sessionId: string): PendingRequest | undefined {
  try {
    const raw = window.sessionStorage.getItem(PARKED_PREFIX + sessionId);
    return raw ? (JSON.parse(raw) as PendingRequest) : undefined;
  } catch {
    return undefined;
  }
}

function saveParked(sessionId: string, request: PendingRequest): void {
  try {
    window.sessionStorage.setItem(PARKED_PREFIX + sessionId, JSON.stringify(request));
  } catch {
    // Not being able to persist costs the parked state a reload, nothing more.
  }
}

/**
 * The structured answer, as the runtime would read it.
 *
 * Both conditions, because the runtime applies both: the payload has to be under the
 * extension's own URI, and it has to say what it is. Returns nothing otherwise, which
 * is what the runtime does — silently.
 */
function readAnswer(
  hitl: Record<string, unknown> | undefined,
): { id: string; answers: string[][] } | undefined {
  const payload = hitl?.[HITL_EXTENSION_URI];
  if (typeof payload !== "object" || payload === null) return undefined;
  const body = payload as Record<string, unknown>;
  if (body.type !== "ask_user_response" || typeof body.id !== "string") return undefined;
  const answers = Array.isArray(body.answers)
    ? body.answers.map((entry) => {
        const value = (entry as Record<string, unknown>)?.answer;
        return Array.isArray(value) ? value.map(String) : [];
      })
    : [];
  return { id: body.id, answers };
}

function clearParked(sessionId: string): void {
  try {
    window.sessionStorage.removeItem(PARKED_PREFIX + sessionId);
  } catch {
    // See above.
  }
}

function loadTranscript(sessionId: string): ChatMessage[] | null {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_PREFIX + sessionId);
    return raw ? (JSON.parse(raw) as ChatMessage[]) : null;
  } catch {
    // Storage can be unavailable or hold something unparseable; either way the
    // conversation simply starts from its seed.
    return null;
  }
}

function saveTranscript(sessionId: string, messages: ChatMessage[]): void {
  try {
    window.sessionStorage.setItem(STORAGE_PREFIX + sessionId, JSON.stringify(messages));
  } catch {
    // Not being able to persist costs the transcript a reload, nothing more.
  }
}

/**
 * Conversations that already exist when the app opens.
 *
 * Built fresh per call so one test's turns cannot leak into the next.
 */
const SEEDED_TRANSCRIPTS: Record<string, () => ChatMessage[]> = {
  // Keyed by `namespace/instance-id`, which is what a conversation is addressed
  // by now — the same key `conversationKey` builds. This is the first instance in
  // `mockAgentInstances`, so the fixture agent a reader opens first has a
  // conversation already in it.
  "kagent/6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44": () => [
    message("seed-1-user", "user", "Why is checkout crashlooping?", "seed-task-1"),
    dataMessage("seed-1-call", "seed-task-1", "tool_call", {
      id: "call-seed-1",
      name: "k8s_get_events",
      // Out of alphabetical order on purpose. These args reach the app as a protobuf
      // `Struct`, whose field order is whatever the sender emitted — for a Go map, a
      // different order every marshal. A fixture in tidy order would let a renderer
      // that prints the wire order look correct while the real page reshuffled.
      args: { resource: "deployment/checkout", namespace: "shop" },
    }),
    dataMessage("seed-1-result", "seed-task-1", "tool_result", {
      id: "call-seed-1",
      name: "k8s_get_events",
      // Deliberately the other spelling. The controller sends `output`, which the
      // live turn above uses; this seeded history keeps `result` so both shapes
      // stay exercised — a renderer that only understood one of them showed a
      // page of real tool output as `{}`.
      response: {
        result: "BackOff: liveness probe failed on :8080/healthz",
        isError: false,
      },
    }),
    message(
      "seed-1-reply",
      "agent",
      [
        "The checkout deployment is failing its liveness probe on port 8080. The container starts, but /healthz never returns 200, so the kubelet restarts it.",
        "",
        "```mermaid",
        "flowchart TD",
        "  A[Container starts] --> B{/healthz returns 200?}",
        "  B -- yes --> C[Ready]",
        "  B -- no --> D[Kubelet restarts container]",
        "  D --> A",
        "```",
      ].join("\n"),
      "seed-task-1",
    ),
  ],
  /*
   * An *unnamed* conversation with something said in it.
   *
   * Here so the derived title has anything to derive from. A conversation nobody
   * has named is titled from its first message, and that is only free where the
   * transcript has already been read — which is this page and nowhere else. With
   * every seeded transcript belonging to a named conversation, the fallback would
   * be unreachable and untestable while looking implemented.
   */
  "kagent/2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0": () => [
    message(
      "seed-2-user",
      "user",
      "Summarise last night's deploy for the incident channel, and note anything that rolled back",
      "seed-task-2",
    ),
    message(
      "seed-2-reply",
      "agent",
      "Two services rolled at 23:40; both reached ready. Nothing rolled back.",
      "seed-task-2",
    ),
  ],
};

function message(
  id: string,
  role: ChatMessage["role"],
  text: string,
  taskId: string,
): ChatMessage {
  return {
    id,
    role,
    taskId,
    createdAt: new Date().toISOString(),
    parts: [{ kind: "text", text }],
  };
}

function dataMessage(
  id: string,
  taskId: string,
  dataKind: "tool_call" | "tool_result",
  data: Record<string, unknown>,
): ChatMessage {
  return {
    id,
    role: "agent",
    taskId,
    createdAt: new Date().toISOString(),
    parts: [{ kind: "data", dataKind, data }],
  };
}

function appendText(target: ChatMessage, chunk: string): void {
  const first = target.parts[0];
  if (first?.kind === "text") first.text += chunk;
}

/** A detached copy, safe to hand out for a message this client still mutates. */
function snapshot(message: ChatMessage): ChatMessage {
  return { ...message, parts: message.parts.map((part) => ({ ...part })) };
}

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });
}

/**
 * Waits, then reports whether the caller gave up while we waited.
 *
 * The wait itself is interruptible, so cancelling a turn stops it now rather
 * than at the end of the current step — which for the slow scenario would be
 * over a second of a button that looks stuck.
 */
async function stopped(signal: AbortSignal | undefined, ms: number): Promise<boolean> {
  await delay(ms, signal);
  return signal?.aborted === true;
}

/**
 * Refuses a conversation opened with a share token the backend never issued.
 *
 * The fixture enforcing what the controller enforces, for the reason this codebase
 * keeps rediscovering: a mock that served the conversation to *any* token would let
 * a build that mangled or dropped the token pass every fixture-backed test, and the
 * miss would read on screen as success.
 *
 * What it cannot check is the header. This client is a client-side fake — it builds
 * no request — so it reads the registration directly rather than seeing what
 * travelled. Proving the header reaches a backend needs the live suite; that gap is
 * recorded in `playwright/DEFERRED.md` rather than papered over here.
 */
function refuseAnInvalidShare(conversation: ChatConversationRef): void {
  const token = agentInstanceShareToken(conversation.namespace, conversation.id);
  if (!token) return;

  const share = instanceShareForToken(token);
  if (!share || share.agentInstanceId !== conversation.id) {
    throw new ApiError("invalid or expired share token", {
      kind: "http",
      status: 403,
      url: "AgentInstanceService/share",
    });
  }
}
