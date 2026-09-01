import type { ChatMessage } from "./types";

/**
 * Putting a legacy task's messages back in the order they happened.
 *
 * New kagent records carry `kagent.dev/timeline-position`. This inference remains
 * for older records until A2A's native timeline and artifact generation ranges land:
 * https://github.com/a2aproject/A2A/pull/2129
 *
 * `ListTasks` answers with two parallel lists: every message the reader sent is in
 * `history`, every message the agent produced is in `artifacts`, and nothing in the
 * response says how to interleave them. Concatenating them — which is what this used to
 * do — renders every answer above the question it answers as soon as a task holds more
 * than one reader turn, which `ask_user` guarantees.
 *
 * There is no key to do this properly with, and the gap is filed as
 * https://github.com/kagent-dev/kagent/issues/2584:
 *
 * - every message in a task carries the task's single `status.timestamp`, so time puts
 *   them all in one bucket;
 * - artifact ids are UUIDv7 and sort correctly, but `history` ids are minted by the
 *   client that sent the message and are UUIDv4, carrying no time at all;
 * - the answer's own correlation id (`ask_user_response`) and the call's id (the
 *   model's `call_…`) do not refer to each other.
 *
 * So this is inference, and only from position. **Delete it when the gateway grows an
 * ordering key** — one interleaved sequence, a sequence number, or a per-entry
 * timestamp — rather than building on it.
 *
 * ## The inference
 *
 * A reader's answer belongs to the `ask_user` round it answered, and rounds are
 * answered in the order they were asked: the runtime pairs answers to questions
 * positionally, and an instance holds one non-terminal task at a time, so the *n*th
 * answer answers the *n*th round. Within a round the answer goes immediately before
 * that round's last `ask_user` result — the one reporting what was answered — which
 * puts it after the call and after the result that was still pending.
 *
 * Anything left over is appended rather than dropped: a transcript missing a message is
 * worse than one holding it in the wrong place, and a shape this does not recognise
 * should degrade to the old behaviour rather than lose a turn.
 */
export function interleaveTaskMessages(
  /** Reader turns that opened the task, in the order `history` gave them. */
  opening: readonly ChatMessage[],
  /** Reader turns that answered an `ask_user`, in the order they were asked. */
  answers: readonly ChatMessage[],
  /** Everything the agent produced, already time-ordered by its UUIDv7 ids. */
  agent: readonly ChatMessage[],
): ChatMessage[] {
  const ordered: ChatMessage[] = [...opening];
  const unplaced = [...answers];

  let at = 0;
  while (at < agent.length) {
    if (!isAskUserCall(agent[at])) {
      ordered.push(agent[at]);
      at += 1;
      continue;
    }

    // The round is this call and everything up to the next one.
    let end = at + 1;
    while (end < agent.length && !isAskUserCall(agent[end])) end += 1;
    const round = agent.slice(at, end);

    // Before the last result of the round, which is the one carrying the answer.
    // Falling back to the end of the round keeps the answer inside the round it
    // belongs to even when the results are not the shape expected here.
    let before = round.length;
    for (let index = round.length - 1; index > 0; index -= 1) {
      if (isAskUserResult(round[index])) {
        before = index;
        break;
      }
    }

    ordered.push(...round.slice(0, before));
    const answer = unplaced.shift();
    if (answer) ordered.push(answer);
    ordered.push(...round.slice(before));
    at = end;
  }

  // More answers than rounds recognised: keep them rather than lose them.
  ordered.push(...unplaced);
  return ordered;
}

const isAskUserCall = (message: ChatMessage) => hasAskUser(message, "tool_call");
const isAskUserResult = (message: ChatMessage) => hasAskUser(message, "tool_result");

function hasAskUser(message: ChatMessage, kind: "tool_call" | "tool_result"): boolean {
  return message.parts.some(
    (part) => part.kind === "data" && part.dataKind === kind && part.data.name === "ask_user",
  );
}
