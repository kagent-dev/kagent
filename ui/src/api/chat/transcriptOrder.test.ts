import { describe, expect, it } from "vitest";
import { interleaveTaskMessages } from "./transcriptOrder";
import type { ChatMessage } from "./types";

const text = (id: string, role: "user" | "agent", body: string): ChatMessage => ({
  id,
  role,
  parts: [{ kind: "text", text: body }],
  createdAt: "2026-08-26T16:53:50.000Z",
});

const askUser = (id: string, kind: "tool_call" | "tool_result"): ChatMessage => ({
  id,
  role: "agent",
  parts: [{ kind: "data", dataKind: kind, data: { name: "ask_user", id: "call_1" } }],
  createdAt: "2026-08-26T16:53:50.000Z",
});

/**
 * The shape read back from a real cluster, which is what this exists for.
 *
 * One task, three reader turns in `history` and seven agent entries in `artifacts`:
 * "ask me a question" opened it, then two `ask_user` rounds of call, pending result and
 * answered result, then the closing reply. Concatenating the two lists — what this
 * replaced — put both answers above every question.
 */
describe("interleaveTaskMessages", () => {
  const opening = [text("u0", "user", "ask me a question")];
  const answers = [
    text("u1", "user", "Personal development"),
    text("u2", "user", "none"),
  ];
  const agent = [
    askUser("a0", "tool_call"),
    askUser("a1", "tool_result"),
    askUser("a2", "tool_result"),
    askUser("a3", "tool_call"),
    askUser("a4", "tool_result"),
    askUser("a5", "tool_result"),
    text("a6", "agent", "Thank you for sharing."),
  ];

  it("puts each answer in the round it answered", () => {
    expect(interleaveTaskMessages(opening, answers, agent).map((m) => m.id)).toEqual([
      "u0",
      "a0", // ask_user called
      "a1", // result: pending
      "u1", // the reader answers
      "a2", // result: answered
      "a3",
      "a4",
      "u2",
      "a5",
      "a6", // the closing reply stays last
    ]);
  });

  it("leaves a task with no questions exactly as it was", () => {
    const plain = [text("a0", "agent", "Hello!")];
    expect(
      interleaveTaskMessages([text("u0", "user", "hi")], [], plain).map((m) => m.id),
    ).toEqual(["u0", "a0"]);
  });

  it("keeps an answer it cannot place rather than dropping it", () => {
    // A shape this does not recognise must degrade to the old behaviour, not lose a
    // turn: a transcript missing a message is worse than one holding it out of order.
    const orphan = [text("u9", "user", "answer to nothing")];
    expect(
      interleaveTaskMessages([], orphan, [text("a0", "agent", "no questions here")]).map(
        (m) => m.id,
      ),
    ).toEqual(["a0", "u9"]);
  });

  it("does not put an answer after a round's closing prose", () => {
    // The last round runs to the end of the task, so "the end of the round" would put
    // the answer below the agent's closing reply. It goes before the last result.
    const oneRound = [
      askUser("a0", "tool_call"),
      askUser("a1", "tool_result"),
      askUser("a2", "tool_result"),
      text("a3", "agent", "Thanks."),
    ];
    expect(
      interleaveTaskMessages([], [text("u1", "user", "yes")], oneRound).map((m) => m.id),
    ).toEqual(["a0", "a1", "u1", "a2", "a3"]);
  });
});
