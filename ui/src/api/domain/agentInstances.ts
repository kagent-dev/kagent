/**
 * A running agent, as the v1alpha3 runtime reports it.
 *
 * An `AgentInstance` is not a custom resource — it is a row in the controller's
 * own database, served by `AgentInstanceService` in
 * `proto/kagent/api/v1alpha1/agent_instances.proto` and implemented in
 * `go/core/v2/agentinstance`. So unlike agents, models and prompt libraries,
 * nothing here arrives inside a `StructuredObject`: the proto message *is* the
 * record, and these types mirror it field for field.
 *
 * ## Two enums, spelled as words
 *
 * `state` and `operation` are proto enums, which the generated code represents as
 * numbers. Numbers are the wrong thing to carry into a page: they sort and compare
 * as integers, they mean nothing in a test failure, and a `Record<number, ...>`
 * lookup on a value this build has never heard of yields `undefined` silently.
 * They are converted once, at the client boundary, into the string unions below —
 * and a value outside them becomes `"unknown"` rather than vanishing, so a UI
 * built against an older proto than the cluster says so instead of rendering a
 * blank cell.
 *
 * ## What "unspecified" means, and what it does not
 *
 * Proto3 gives every enum a zero value, and both of these use it. The controller
 * leaves `operation` at zero when no lifecycle operation is in flight, so
 * `"unspecified"` there reads as "nothing happening" — but that is the *controller's*
 * convention rather than something the wire distinguishes, and a record that was
 * never written would look identical. `state` at zero has no such convention: it
 * means the controller did not say. Both are rendered as their own thing on screen
 * rather than being folded into a plausible-looking default.
 */

/**
 * Where an instance is in its life.
 *
 * The six named values are `AgentInstanceState` in the proto, in its order.
 * `"unknown"` is this client's, for an enum member added after this build.
 */
export type AgentInstanceState =
  | "unspecified"
  | "creating"
  | "ready"
  | "suspended"
  | "failed"
  | "deleting"
  | "deleted"
  | "unknown";

/**
 * The lifecycle operation currently claimed on an instance, if any.
 *
 * The controller claims one before it acts and clears it when it finishes
 * (`claim`/`finish` in `go/core/v2/agentinstance/workflow.go`), so a non-`
 * unspecified` value means something is in flight *right now* — and that a second
 * operation asked for meanwhile will be refused with `Aborted`.
 */
export type AgentInstanceOperation =
  | "unspecified"
  | "create"
  | "suspend"
  | "resume"
  | "delete"
  | "unknown";

/**
 * Why an instance failed, as the controller recorded it.
 *
 * Both halves are optional because both are proto3 strings: the message can be
 * present with nothing in it, which is a different thing from being absent and is
 * worth telling a reader about rather than papering over.
 */
export interface AgentInstanceFailure {
  /** A short machine-ish cause, e.g. the actor status that blocked the operation. */
  reason?: string;
  /** The full explanation, where there is one. */
  message?: string;
}

/**
 * One conversation with an agent. Mirrors the `AgentInstance` proto message.
 *
 * An instance is a *conversation*, not an agent. The A2A gateway files every task
 * under the instance as the task's `contextId`, so an instance holds exactly one
 * thread of turns and a second conversation with the same agent is a second
 * instance. The durable, runnable agent is the `(AgentTemplate, Harness)` pair it
 * was cut from — see `domain/agentPairs`.
 */
export interface AgentInstance {
  /** A UUID. The controller rejects anything else — `validateIdentity` parses it. */
  id: string;
  namespace: string;
  /**
   * The reader's own title for this conversation. Empty means unnamed.
   *
   * Empty is ordinary rather than a gap: the column was added after the table
   * existed, so every conversation created before it reads back empty, and
   * `CreateAgentInstance` still accepts a create with no name. Never render it raw
   * — `conversationTitle` in `components/agent-instances/instanceLabels` is what
   * turns it, or its absence, into something to put on screen.
   */
  name: string;
  /** Who created it. Empty on a cluster with no authentication in front. */
  creator: string;
  /** `namespace/name` of the AgentHarness it runs, when the record carries one. */
  harness?: string;
  /**
   * `namespace/name` of the AgentTemplate it was cut from.
   *
   * With the harness above, this is the agent: the pair is what `ListAgentInstances`
   * narrows on, and what an agent's page is addressed by.
   */
  agentTemplate?: string;
  /** The runtime revision this instance was prepared against. */
  preparedRevision?: string;
  /** Where its A2A endpoint is served, for a caller that wants to reach it. */
  a2aAuthority?: string;
  state: AgentInstanceState;
  operation: AgentInstanceOperation;
  /** Set only when the controller recorded a failure. */
  failure?: AgentInstanceFailure;
  /** RFC3339, or empty when the record carried no timestamp. */
  createdAt: string;
  /** RFC3339, or empty when the record carried no timestamp. */
  updatedAt: string;
  labels: Record<string, string>;
}

/**
 * Whether an instance can be suspended right now.
 *
 * Not a guess about the UI's preferences — it is the controller's precondition,
 * copied. `ActorWorkflow.Suspend` claims the instance from `READY` and no other
 * state, and the claim also requires that no operation is already in flight; a
 * request from anywhere else comes back `Aborted` with "conflicting lifecycle
 * operation". Asking here means the button is disabled with a reason rather than
 * enabled into a refusal.
 */
export function canSuspend(instance: AgentInstance): boolean {
  return instance.state === "ready" && instance.operation === "unspecified";
}

/** Whether an instance can be resumed right now — the mirror of `canSuspend`. */
export function canResume(instance: AgentInstance): boolean {
  return instance.state === "suspended" && instance.operation === "unspecified";
}

/**
 * Why the lifecycle buttons are disabled, in words a reader can act on.
 *
 * Returns `undefined` when the operation is available. A disabled control with no
 * explanation is the thing this exists to avoid: "Suspend" greyed out on a failed
 * instance looks like a permission problem when it is a state machine.
 */
export function lifecycleBlockedReason(
  instance: AgentInstance,
  action: "suspend" | "resume",
): string | undefined {
  if (instance.operation !== "unspecified") {
    return `A ${instance.operation} operation is already in progress. The controller refuses a second one until it finishes.`;
  }

  const wanted = action === "suspend" ? "ready" : "suspended";
  if (instance.state === wanted) return undefined;

  if (instance.state === "unspecified") {
    return `The controller did not report a state for this instance, so it cannot be ${action}d.`;
  }
  return `Only a ${wanted} instance can be ${action}d. This one is ${instance.state}.`;
}

/**
 * The longest name the controller will store, counted the way it counts.
 *
 * Runes, not bytes — `utf8.RuneCountInString` in `validateName` — so a title in a
 * non-Latin script gets the same number of characters as an English one rather
 * than a third as many.
 */
export const MAX_CONVERSATION_NAME_LENGTH = 200;

/**
 * Every code point Go's `unicode.IsControl` matches: Unicode category Cc.
 *
 * Written as escapes rather than as literal characters, because a source file
 * carrying a NUL is a binary file to every tool that touches it.
 */
// eslint-disable-next-line no-control-regex
const CONTROL_CHARACTERS = /[\u0000-\u001f\u007f-\u009f]/;

/**
 * Why the controller would refuse this name, or `undefined` if it would not.
 *
 * The controller's own rules (`validateName`, `go/core/v2/agentinstance/service.go`),
 * copied rather than approximated, so a rename box can say no before the round trip
 * instead of turning an `InvalidArgument` into a red banner.
 *
 * Note what is deliberately *not* done here. Surrounding whitespace is refused
 * rather than trimmed, because the controller refuses it too and because quietly
 * rewriting what somebody typed reads on screen as a rename that did not take. And
 * an empty name is valid: it means unnamed, and it is how a title is cleared.
 */
export function conversationNameProblem(name: string): string | undefined {
  if (name === "") return undefined;
  if (name.trim() !== name) {
    return "A name cannot start or end with a space.";
  }
  // Spread rather than `.length`: a string's length counts UTF-16 units, so an
  // emoji would count double against the controller's rune limit and be refused
  // here at a length the controller accepts.
  if ([...name].length > MAX_CONVERSATION_NAME_LENGTH) {
    return `A name can be at most ${MAX_CONVERSATION_NAME_LENGTH} characters.`;
  }
  if (CONTROL_CHARACTERS.test(name)) {
    return "A name cannot contain control characters.";
  }
  return undefined;
}

/**
 * What a share link over an AgentInstance allows.
 *
 * `readOnly` covers A2A get, list and subscribe; `readWrite` also allows send and
 * cancel. The controller stores these as `READ_ONLY` and `READ_WRITE` under a CHECK
 * constraint, and the interceptor treats anything that is not `READ_WRITE` as
 * read-only — so an unrecognised value is the safe one rather than the permissive
 * one.
 */
export type AgentInstanceSharePermission = "readOnly" | "readWrite";

/** One share link over one instance. The token itself is returned only on create. */
export interface AgentInstanceShare {
  id: string;
  namespace: string;
  agentInstanceId: string;
  creator: string;
  permission: AgentInstanceSharePermission;
  /** RFC3339, or empty when the record carried no timestamp. */
  createdAt: string;
}

/**
 * A newly created share, with the one thing that is never shown again.
 *
 * The controller returns the token only from the create call — only its digest is
 * stored — so a caller that does not keep it here cannot offer it later. That is
 * why this is a separate shape rather than a field on `AgentInstanceShare`.
 */
export interface CreatedAgentInstanceShare {
  share: AgentInstanceShare;
  token: string;
}
