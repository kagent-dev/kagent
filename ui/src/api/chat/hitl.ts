/**
 * The human-in-the-loop extension: how an agent asks the reader something.
 *
 * A2A carries this as a *message extension* rather than as a part or a state, and
 * the whole of it is one URI, one metadata key and two payload shapes. Reading and
 * writing them is here, apart from the transport, because every one of the ways to
 * get this wrong is silent — a build that misses one appears to work and answers
 * nothing.
 *
 * ## The header is what makes the question exist
 *
 * Measured on a cluster on 2026-08-24, and it is the opposite of what it looks
 * like. `A2A-Extensions` matters on the call that *asks*, not on the call that
 * reads:
 *
 * - A turn sent **without** the header parks with a status message carrying the
 *   question as bare prose and no metadata at all — there is nothing to render, and
 *   no correlation id, so the question can never be answered. Re-reading it with
 *   the header does not help: the payload was never attached.
 * - A turn sent **with** it parks carrying
 *   `metadata["…/hitl/v1"] = {type, id, questions:[{question, choices, multiple}]}`,
 *   and that payload is **persisted** — `ListTasks` returns it afterwards whether
 *   or not the read asks for the extension.
 *
 * So the header goes on every call this client makes. Sending it on a read is
 * harmless; failing to send it on a send is unrecoverable for that turn.
 *
 * ## And the answer has to declare it too
 *
 * `rawHitlMap` in `go/adk/pkg/a2a/hitl.go` ignores the metadata unless the *message*
 * lists the URI in its `extensions`. An answer that omits it is forwarded to the
 * agent as ordinary text: the turn resumes, the agent replies, and the structured
 * answer was never delivered. Nothing reports it.
 */

/** The versioned URI. Both the header value and the metadata key. */
export const HITL_EXTENSION_URI = "https://kagent.dev/extensions/hitl/v1";

/** The header that activates the extension for a call. */
export const HITL_EXTENSION_HEADER = "A2A-Extensions";

/** One thing the agent wants to know. */
export interface HitlQuestion {
  question: string;
  /** The options offered. May be empty, which means free text. */
  choices: string[];
  /** Whether more than one choice may be picked. */
  multiple: boolean;
}

/** A tool the agent wants permission to run. */
export interface HitlTool {
  id: string;
  name: string;
  args: Record<string, unknown>;
}

/** One human decision for a requested tool invocation. */
export interface ToolApprovalDecision {
  id: string;
  approved: boolean;
  rejectionReason?: string;
}

/** A completed approval exchange, kept as one visual transcript event. */
export interface ToolApprovalRecord {
  tools: HitlTool[];
  decisions: ToolApprovalDecision[];
  /** The subagent that asked, when one did. */
  askedBy?: string;
}

/** A completed question exchange, kept as one visual transcript event. */
export interface AskUserRecord {
  questions: HitlQuestion[];
  /** One answer array per question, in the same positional order as the request. */
  answers: string[][];
  /** The subagent that asked, when one did. */
  askedBy?: string;
}

/** The structured answer carried by a reader's A2A Message. */
export interface AskUserResponse {
  requestId: string;
  answers: string[][];
}

/**
 * What the agent is waiting for.
 *
 * `unknown` is a real member and not a failure: this extension already carries two
 * request types and will carry more, and a build that met a third one and rendered
 * nothing would look like a stalled agent. It is shown as "the agent is waiting for
 * something this build does not understand", which is true and actionable — the
 * reader can still give it up.
 */
export type PendingRequest =
  | {
      kind: "ask_user";
      taskId: string;
      /**
       * The correlation id, echoed verbatim in the answer.
       *
       * From the payload and nowhere else. The runtime mints a fresh one per
       * question and matches on it, which is what makes a reply to a question that
       * has already moved on refusable rather than misapplied.
       */
      requestId: string;
      questions: HitlQuestion[];
      /** The subagent that asked, when one did. */
      askedBy?: string;
    }
  | {
      kind: "tool_approval";
      taskId: string;
      tools: HitlTool[];
      hint?: string;
      askedBy?: string;
    }
  | { kind: "unknown"; taskId: string };

/** The metadata of an A2A message, as protobuf-es represents a `Struct`. */
type Metadata = Record<string, unknown> | undefined;

/**
 * The request a parked turn is holding, if this build understands it.
 *
 * `extensions` must list the URI — the runtime's own guard, copied, so that a
 * payload arriving without the declaration is treated the way the agent treats it.
 */
export function readHitlRequest(
  taskId: string,
  metadata: Metadata,
  extensions: readonly string[] | undefined,
): PendingRequest | undefined {
  if (!taskId) return undefined;
  if (!extensions?.includes(HITL_EXTENSION_URI)) return undefined;

  const payload = metadata?.[HITL_EXTENSION_URI];
  if (!isObject(payload)) return undefined;

  const askedBy = nestedSubagent(payload.nested);

  if (payload.type === "ask_user_request") {
    const requestId = typeof payload.id === "string" ? payload.id : "";
    // The runtime refuses its own request without an id, and so does this: a
    // question with nothing to correlate on can be shown and never answered, which
    // is worse than not offering the control.
    if (requestId === "") return { kind: "unknown", taskId };
    return {
      kind: "ask_user",
      taskId,
      requestId,
      questions: readQuestions(payload.questions),
      askedBy,
    };
  }

  if (payload.type === "tool_approval_request") {
    // A nested request displays and decides the child's tools. The top-level list
    // is the parent's remote-agent continuation and is not what the human is
    // authorizing (see the extension's identifier model).
    const nested = isObject(payload.nested) ? payload.nested : undefined;
    const tools = readTools(nested?.tools ?? payload.tools);
    if (tools.length === 0) return { kind: "unknown", taskId };
    return {
      kind: "tool_approval",
      taskId,
      tools,
      hint: typeof payload.hint === "string" ? payload.hint : undefined,
      askedBy,
    };
  }

  return { kind: "unknown", taskId };
}

/**
 * The metadata that carries an answer back.
 *
 * One entry per question, **in the order they were asked** — the runtime pairs them
 * positionally, so a reordered array answers the wrong questions with no error
 * anywhere. Each `answer` is an array because a question may take several choices;
 * one that did not still gets an array of one.
 */
export function askUserAnswer(
  requestId: string,
  answers: readonly string[][],
): Record<string, unknown> {
  return {
    [HITL_EXTENSION_URI]: {
      type: "ask_user_response",
      id: requestId,
      answers: answers.map((answer) => ({ answer: [...answer] })),
    },
  };
}

/** The metadata that resumes a turn waiting for tool approval. */
export function toolApprovalAnswer(
  decisions: readonly ToolApprovalDecision[],
): Record<string, unknown> {
  return {
    [HITL_EXTENSION_URI]: {
      type: "tool_approval_response",
      approvals: decisions.map((decision) => ({
        id: decision.id,
        approved: decision.approved,
        ...(decision.rejectionReason
          ? { rejection_reason: decision.rejectionReason }
          : {}),
      })),
    },
  };
}

/** Reads a tool decision from a user Message that declares the HITL extension. */
export function readToolApprovalResponse(
  metadata: Metadata,
  extensions: readonly string[] | undefined,
): ToolApprovalDecision[] | undefined {
  if (!extensions?.includes(HITL_EXTENSION_URI)) return undefined;

  const payload = metadata?.[HITL_EXTENSION_URI];
  if (!isObject(payload) || payload.type !== "tool_approval_response") return undefined;
  if (!Array.isArray(payload.approvals) || payload.approvals.length === 0) {
    return undefined;
  }

  const decisions: ToolApprovalDecision[] = [];
  for (const entry of payload.approvals) {
    if (!isObject(entry) || typeof entry.id !== "string" || entry.id === "") {
      return undefined;
    }
    if (typeof entry.approved !== "boolean") return undefined;
    decisions.push({
      id: entry.id,
      approved: entry.approved,
      rejectionReason:
        typeof entry.rejection_reason === "string" && entry.rejection_reason !== ""
          ? entry.rejection_reason
          : undefined,
    });
  }
  return decisions;
}

/** Reads an ask-user answer from a user Message that declares the HITL extension. */
export function readAskUserResponse(
  metadata: Metadata,
  extensions: readonly string[] | undefined,
): AskUserResponse | undefined {
  if (!extensions?.includes(HITL_EXTENSION_URI)) return undefined;

  const payload = metadata?.[HITL_EXTENSION_URI];
  if (!isObject(payload) || payload.type !== "ask_user_response") return undefined;
  if (typeof payload.id !== "string" || payload.id === "") return undefined;
  if (!Array.isArray(payload.answers) || payload.answers.length === 0) return undefined;

  const answers: string[][] = [];
  for (const entry of payload.answers) {
    if (!isObject(entry) || !Array.isArray(entry.answer)) return undefined;
    const answer = entry.answer.filter((value): value is string => typeof value === "string");
    if (answer.length !== entry.answer.length) return undefined;
    answers.push(answer);
  }
  return { requestId: payload.id, answers };
}

/** Human-readable fallback text carried beside a structured approval response. */
export function toolApprovalText(
  tools: readonly HitlTool[],
  decisions: readonly ToolApprovalDecision[],
): string {
  const names = new Map(tools.map((tool) => [tool.id, tool.name]));
  return decisions
    .map(
      (decision) => {
        const status = `${decision.approved ? "Approved" : "Rejected"}: ${names.get(decision.id) ?? "tool"}`;
        return !decision.approved && decision.rejectionReason
          ? `${status}\nReason: ${decision.rejectionReason}`
          : status;
      },
    )
    .join("\n");
}

/**
 * The prose that goes beside a structured answer.
 *
 * `parts` is what the transcript shows; the metadata is what the agent acts on. They
 * are written from the same choices so the conversation reads as what happened
 * rather than as an empty message followed by an agent that somehow knew.
 */
export function answerText(answers: readonly string[][]): string {
  return answers
    .map((answer) => answer.join(", "))
    .filter((line) => line !== "")
    .join("\n");
}

function readQuestions(value: unknown): HitlQuestion[] {
  if (!Array.isArray(value)) return [];
  return value.filter(isObject).map((entry) => ({
    question: typeof entry.question === "string" ? entry.question : "",
    choices: Array.isArray(entry.choices)
      ? entry.choices.filter((choice): choice is string => typeof choice === "string")
      : [],
    // Absent means one answer. Stated rather than assumed, because rendering a
    // single-choice question as a multi-select produces an answer array the agent
    // did not ask for.
    multiple: entry.multiple === true,
  }));
}

function readTools(value: unknown): HitlTool[] {
  if (!Array.isArray(value)) return [];
  return value.filter(isObject).map((entry) => ({
    id: typeof entry.id === "string" ? entry.id : "",
    name: typeof entry.name === "string" ? entry.name : "",
    args: isObject(entry.args) ? entry.args : {},
  }));
}

/** Which subagent asked, when the request came from one. */
function nestedSubagent(value: unknown): string | undefined {
  if (!isObject(value)) return undefined;
  const name = value.subagent_name;
  return typeof name === "string" && name !== "" ? name : undefined;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
