// A2A chat streaming mock. Unlike the /api data calls (server-side → stub), the
// chat SSE call `POST /a2a/<ns>/<name>` is a real browser fetch, so page.route
// CAN intercept it. We fulfill it with a text/event-stream body of JSON-RPC
// frames (see a2aClient.ts processSSEStream): frames are "\n\n"-delimited, each
// line prefixed "data: ", each payload unwrapped as `.result`.

import { type Page } from "@playwright/test";

// contextId must match the session id the stub's POST /api/sessions returns.
const CONTEXT_ID = "e2e-session";
const TASK_ID = "e2e-task";

interface ToolCall {
  name: string;
  args?: Record<string, unknown>;
  result?: string;
}

function frame(event: unknown): string {
  return `data: ${JSON.stringify({ jsonrpc: "2.0", id: "1", result: event })}\n\n`;
}

function statusUpdate(status: unknown, final = false): unknown {
  return { kind: "status-update", taskId: TASK_ID, contextId: CONTEXT_ID, final, status };
}

function agentMessage(parts: unknown[], messageId?: number): unknown {
  // A numeric messageId on the terminal text message makes the feedback (thumbs)
  // buttons render on the agent reply.
  return { kind: "message", role: "agent", parts, contextId: CONTEXT_ID, taskId: TASK_ID, messageId, metadata: {} };
}

/** Build the SSE body: optional tool call (request + response), then a final agent text. */
function buildStream({ text, tool }: { text: string; tool?: ToolCall }): string {
  const frames: string[] = [];
  if (tool) {
    frames.push(
      frame(
        statusUpdate({
          state: "working",
          message: agentMessage([
            {
              kind: "data",
              data: { id: "call-1", name: tool.name, args: tool.args ?? {} },
              metadata: { adk_type: "function_call" },
            },
          ]),
        }),
      ),
    );
    frames.push(
      frame(
        statusUpdate({
          state: "working",
          message: agentMessage([
            {
              kind: "data",
              data: { id: "call-1", name: tool.name, response: { result: tool.result ?? "ok", isError: false } },
              metadata: { adk_type: "function_response" },
            },
          ]),
        }),
      ),
    );
  }
  // Terminal frame settles the turn and carries the agent's text reply.
  frames.push(
    frame(
      statusUpdate(
        { state: "completed", message: agentMessage([{ kind: "text", text }], 1) },
        true,
      ),
    ),
  );
  return frames.join("") + "data: [DONE]\n\n";
}

/** Intercept the chat SSE call and reply with an agent message (and optional tool call). */
export async function mockAgentReply(
  page: Page,
  opts: { text: string; tool?: ToolCall },
): Promise<void> {
  const body = buildStream(opts);
  const handler = (route: import("@playwright/test").Route) =>
    route.fulfill({ status: 200, contentType: "text/event-stream", body });
  await page.route("**/a2a/**", handler);
  await page.route("**/a2a-sandboxes/**", handler);
}

/** Intercept the chat SSE call and fail it (network error), for the failure path. */
export async function mockAgentStreamError(page: Page): Promise<void> {
  const handler = (route: import("@playwright/test").Route) => route.abort("failed");
  await page.route("**/a2a/**", handler);
  await page.route("**/a2a-sandboxes/**", handler);
}
