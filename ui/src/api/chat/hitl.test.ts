import { describe, expect, it } from "vitest";
import {
  HITL_EXTENSION_URI,
  answerText,
  askUserAnswer,
  readAskUserResponse,
  readHitlRequest,
  readToolApprovalResponse,
  toolApprovalAnswer,
  toolApprovalText,
} from "./hitl";

/**
 * Reading a question and writing an answer.
 *
 * Every case here is a way to get the extension wrong that **reports nothing**: the
 * turn resumes, the agent replies, and the structured answer was never delivered or
 * was applied to the wrong question. There is no error to catch and no screenshot
 * that shows it, so these are the instrument.
 *
 * The payloads are the ones captured from a cluster on 2026-08-24, not invented.
 */

/** The parked message as the wire carries it, once the ask activated the extension. */
const ASK_METADATA = {
  [HITL_EXTENSION_URI]: {
    type: "ask_user_request",
    id: "adk-185924e6-f291-4c9a-9937-b8133ebe47bc",
    questions: [
      {
        choices: ["Small", "Medium", "Large"],
        multiple: false,
        question: "What size pizza would you like?",
      },
    ],
  },
};

const ACTIVE = [HITL_EXTENSION_URI];

describe("readHitlRequest", () => {
  it("reads the question, its choices and whether it takes more than one", () => {
    const request = readHitlRequest("task-1", ASK_METADATA, ACTIVE);
    expect(request).toEqual({
      kind: "ask_user",
      taskId: "task-1",
      requestId: "adk-185924e6-f291-4c9a-9937-b8133ebe47bc",
      questions: [
        {
          question: "What size pizza would you like?",
          choices: ["Small", "Medium", "Large"],
          multiple: false,
        },
      ],
      askedBy: undefined,
    });
  });

  it("ignores a payload on a message that does not declare the extension", () => {
    /*
     * The runtime's own guard, copied — `rawHitlMap` in `go/adk/pkg/a2a/hitl.go`
     * checks `Extensions` before it reads `Metadata` at all. Reading it here anyway
     * would let this client offer choices for a question the agent will not accept
     * an answer to.
     */
    expect(readHitlRequest("task-1", ASK_METADATA, [])).toBeUndefined();
    expect(readHitlRequest("task-1", ASK_METADATA, undefined)).toBeUndefined();
  });

  it("refuses a question with no correlation id rather than offering to answer it", () => {
    // The runtime mints one per question and matches on it. Without one there is
    // nothing to route a reply to, so buttons here would post into a void.
    const request = readHitlRequest(
      "task-1",
      { [HITL_EXTENSION_URI]: { type: "ask_user_request", questions: [] } },
      ACTIVE,
    );
    expect(request).toEqual({ kind: "unknown", taskId: "task-1" });
  });

  it("says `multiple` only when the payload does", () => {
    // Absent means one answer. Rendering a single-choice question as a multi-select
    // sends an array the agent did not ask for.
    const [single, multi] = readMultiple();
    expect(single.multiple).toBe(false);
    expect(multi.multiple).toBe(true);
  });

  it("recognises a tool-approval request as something else, not as a question", () => {
    // Different payload, different response shape (`approvals[]`). Treating it as an
    // `ask_user` would offer choices whose answer the runtime cannot read.
    const request = readHitlRequest(
      "task-1",
      {
        [HITL_EXTENSION_URI]: {
          type: "tool_approval_request",
          hint: "This will delete a namespace.",
          tools: [{ id: "t1", name: "k8s_delete", args: { namespace: "shop" } }],
        },
      },
      ACTIVE,
    );
    expect(request).toMatchObject({
      kind: "tool_approval",
      hint: "This will delete a namespace.",
      tools: [{ id: "t1", name: "k8s_delete", args: { namespace: "shop" } }],
    });
  });

  it("names a request type it has never heard of instead of rendering nothing", () => {
    // A build older than the extension. An unrecognised request that rendered
    // nothing would look exactly like an agent that had stalled.
    expect(
      readHitlRequest("task-1", { [HITL_EXTENSION_URI]: { type: "future_thing" } }, ACTIVE),
    ).toEqual({ kind: "unknown", taskId: "task-1" });
  });

  it("carries the subagent that asked, when one did", () => {
    const request = readHitlRequest(
      "task-1",
      {
        [HITL_EXTENSION_URI]: {
          ...ASK_METADATA[HITL_EXTENSION_URI],
          nested: { subagent_name: "billing-agent" },
        },
      },
      ACTIVE,
    );
    expect(request).toMatchObject({ askedBy: "billing-agent" });
  });

  it("shows the child operations for a nested approval", () => {
    const request = readHitlRequest(
      "task-1",
      {
        [HITL_EXTENSION_URI]: {
          type: "tool_approval_request",
          tools: [{ id: "parent", name: "k8s_agent", args: {} }],
          nested: {
            subagent_name: "k8s_agent",
            tools: [{ id: "child", name: "delete_pod", args: { name: "old" } }],
          },
        },
      },
      ACTIVE,
    );
    expect(request).toMatchObject({
      kind: "tool_approval",
      askedBy: "k8s_agent",
      tools: [{ id: "child", name: "delete_pod", args: { name: "old" } }],
    });
  });

  it("reports nothing for a turn with no task to answer against", () => {
    expect(readHitlRequest("", ASK_METADATA, ACTIVE)).toBeUndefined();
  });
});

describe("askUserAnswer", () => {
  it("echoes the request id verbatim, which is what correlates the reply", () => {
    // The runtime mints a fresh id per question and matches on it — so a reply
    // carrying a stale or invented one is refused rather than misapplied, and one
    // carrying the right one resumes exactly the turn that asked.
    const payload = askUserAnswer("adk-1", [["Medium"]]) as Record<string, unknown>;
    expect(payload[HITL_EXTENSION_URI]).toEqual({
      type: "ask_user_response",
      id: "adk-1",
      answers: [{ answer: ["Medium"] }],
    });
  });

  it("keeps one entry per question, in order", () => {
    // Paired positionally by the runtime. A reordered or short array answers the
    // wrong question, and nothing anywhere reports it.
    const payload = askUserAnswer("adk-1", [
      ["Large"],
      ["Pepperoni", "Mushroom"],
    ]) as Record<string, Record<string, unknown>>;
    expect(payload[HITL_EXTENSION_URI].answers).toEqual([
      { answer: ["Large"] },
      { answer: ["Pepperoni", "Mushroom"] },
    ]);
  });

  it("keys the payload on the extension's own URI", () => {
    // Not a convenience: `rawHitlMap` looks the payload up by this exact string, so
    // a metadata object under any other key is invisible to the runtime.
    expect(Object.keys(askUserAnswer("adk-1", [["Medium"]]))).toEqual([
      HITL_EXTENSION_URI,
    ]);
  });
});

describe("readAskUserResponse", () => {
  it("reads the correlated positional answers from declared extension metadata", () => {
    expect(
      readAskUserResponse(
        {
          [HITL_EXTENSION_URI]: {
            type: "ask_user_response",
            id: "ask-1",
            answers: [{ answer: ["Large"] }, { answer: ["Mushroom", "Pineapple"] }],
          },
        },
        [HITL_EXTENSION_URI],
      ),
    ).toEqual({
      requestId: "ask-1",
      answers: [["Large"], ["Mushroom", "Pineapple"]],
    });
  });

  it("does not read undeclared or malformed response metadata", () => {
    const metadata = {
      [HITL_EXTENSION_URI]: {
        type: "ask_user_response",
        id: "ask-1",
        answers: [{ answer: ["Large"] }],
      },
    };
    expect(readAskUserResponse(metadata, [])).toBeUndefined();
    expect(
      readAskUserResponse(
        {
          [HITL_EXTENSION_URI]: {
            type: "ask_user_response",
            id: "ask-1",
            answers: [{ answer: [7] }],
          },
        },
        [HITL_EXTENSION_URI],
      ),
    ).toBeUndefined();
  });
});

describe("answerText", () => {
  it("writes the prose from the same choices as the payload", () => {
    // `parts` is what the transcript shows and the metadata is what the agent acts
    // on. Written apart, they drift, and the conversation reads as an empty message
    // followed by an agent that somehow knew what was chosen.
    expect(answerText([["Large"], ["Pepperoni", "Mushroom"]])).toBe(
      "Large\nPepperoni, Mushroom",
    );
  });
});

describe("toolApprovalAnswer", () => {
  it("decides each requested invocation by its opaque id", () => {
    expect(
      toolApprovalAnswer([
        { id: "call-1", approved: true },
        { id: "call-2", approved: false, rejectionReason: "too broad" },
      ]),
    ).toEqual({
      [HITL_EXTENSION_URI]: {
        type: "tool_approval_response",
        approvals: [
          { id: "call-1", approved: true },
          { id: "call-2", approved: false, rejection_reason: "too broad" },
        ],
      },
    });
  });

  it("writes the same decisions as readable transcript text", () => {
    expect(
      toolApprovalText(
        [
          { id: "call-1", name: "read", args: {} },
          { id: "call-2", name: "delete", args: {} },
        ],
        [
          { id: "call-1", approved: true },
          { id: "call-2", approved: false, rejectionReason: "still in use" },
        ],
      ),
    ).toBe("Approved: read\nRejected: delete\nReason: still in use");
  });
});

describe("readToolApprovalResponse", () => {
  it("reads structured decisions without relying on the fallback text", () => {
    expect(
      readToolApprovalResponse(
        {
          [HITL_EXTENSION_URI]: {
            type: "tool_approval_response",
            approvals: [{ id: "call-1", approved: false, rejection_reason: "unsafe" }],
          },
        },
        ACTIVE,
      ),
    ).toEqual([{ id: "call-1", approved: false, rejectionReason: "unsafe" }]);
  });
});

function readMultiple() {
  const request = readHitlRequest(
    "task-1",
    {
      [HITL_EXTENSION_URI]: {
        type: "ask_user_request",
        id: "adk-1",
        questions: [
          { question: "Size?", choices: ["Small"] },
          { question: "Toppings?", choices: ["Pepperoni"], multiple: true },
        ],
      },
    },
    ACTIVE,
  );
  if (request?.kind !== "ask_user") throw new Error("expected an ask_user request");
  return request.questions;
}
