import {
  parseSseStream,
  SendMessageRequest,
  StreamResponse,
  type SseEvent,
} from "@a2a-js/sdk";

import { KagentA2AClient } from "@/lib/a2aClient";

jest.mock("@/lib/utils", () => ({
  getBackendUrl: () => "",
}));

jest.mock("@a2a-js/sdk", () => {
  const actual = jest.requireActual<typeof import("@a2a-js/sdk")>("@a2a-js/sdk");
  return {
    ...actual,
    parseSseStream: jest.fn(),
  };
});

const parseSseStreamMock = jest.mocked(parseSseStream);
const request = SendMessageRequest.fromJSON({});

function events(...items: SseEvent[]): AsyncGenerator<SseEvent, void, undefined> {
  return (async function* generate() {
    for (const item of items) {
      yield item;
    }
  })();
}

function successfulResponse(): Response {
  return {
    ok: true,
    status: 200,
    statusText: "OK",
  } as Response;
}

describe("KagentA2AClient SSE processing", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    global.fetch = jest
      .fn<ReturnType<typeof fetch>, Parameters<typeof fetch>>()
      .mockResolvedValue(successfulResponse());
  });

  it("uses the SDK SSE parser and decodes stream responses", async () => {
    const expected = StreamResponse.fromJSON({
      message: {
        messageId: "message-1",
        role: "ROLE_AGENT",
        parts: [{ text: "hello" }],
      },
    });
    parseSseStreamMock.mockReturnValue(
      events({
        type: "message",
        data: JSON.stringify({ result: StreamResponse.toJSON(expected) }),
      })
    );

    const stream = await new KagentA2AClient().sendMessageStream("default", "agent", request);
    const received: StreamResponse[] = [];
    for await (const item of stream) {
      received.push(item);
    }

    expect(received).toEqual([expected]);
    expect(parseSseStreamMock).toHaveBeenCalledWith(expect.objectContaining({ ok: true }));
  });

  it("propagates JSON-RPC errors instead of treating them as a clean stream end", async () => {
    parseSseStreamMock.mockReturnValue(
      events({
        type: "message",
        data: JSON.stringify({
          error: { code: -32603, message: "agent execution failed" },
        }),
      })
    );

    const stream = await new KagentA2AClient().sendMessageStream("default", "agent", request);

    await expect(stream[Symbol.asyncIterator]().next()).rejects.toThrow(
      "A2A error -32603: agent execution failed"
    );
  });

  it("skips malformed JSON without swallowing later protocol errors", async () => {
    const consoleError = jest.spyOn(console, "error").mockImplementation(() => undefined);
    parseSseStreamMock.mockReturnValue(
      events(
        { type: "message", data: "not-json" },
        {
          type: "error",
          data: JSON.stringify({ code: -32001, message: "task not found" }),
        }
      )
    );

    const stream = await new KagentA2AClient().sendMessageStream("default", "agent", request);

    await expect(stream[Symbol.asyncIterator]().next()).rejects.toThrow("A2A error -32001: task not found");
    expect(consoleError).toHaveBeenCalledWith(
      "❌ Failed to parse SSE data:",
      expect.any(SyntaxError),
      "not-json"
    );
    consoleError.mockRestore();
  });
});
