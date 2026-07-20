// Lightweight dev proxy for the Playwright E2E suite.
//
// It forwards every request to a real kagent backend — deployed into kind by
// playwright/scripts/setup.sh and reached through a port-forward started in
// playwright/setup.ts — EXCEPT the chat A2A stream, which it intercepts and
// answers with a canned SSE reply so the suite never needs a live LLM.
//
//   Next (BACKEND_INTERNAL_URL) ─▶ this proxy ─┬─ /a2a, /a2a-sandboxes ─▶ mocked SSE
//                                              └─ everything else ──────▶ KAGENT_BACKEND_URL
//
// Both the server-side /api calls (Next server actions) and the browser chat call
// (POST /a2a/... via the Next route handler in src/app/a2a/.../route.ts) resolve
// their target through getBackendUrl() → BACKEND_INTERNAL_URL, so a single proxy
// sees both and can split them.
//
// Dependency-free (Node built-in http only) so it runs without a TS/build step.

import { createServer, request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";

const PORT = Number(process.env.STUB_PORT ?? 8899);
// Real kagent backend ORIGIN (no trailing /api — Next sends the full /api/... path,
// so we append req.url as-is). Defaults to the port-forward set up in setup.ts.
const BACKEND_ORIGIN = (process.env.KAGENT_BACKEND_URL ?? "http://127.0.0.1:8083").replace(/\/$/, "");

// region Helpers

const json = (res, status, body) => {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Content-Length": Buffer.byteLength(payload),
  });
  res.end(payload);
};

const readBody = (req) =>
  new Promise((resolve) => {
    let raw = "";
    req.on("data", (chunk) => {
      raw += chunk;
    });
    req.on("end", () => resolve(raw));
    req.on("error", () => resolve(""));
  });

// endregion

// region Chat mock (A2A SSE)
//
// The A2A stream is a text/event-stream of JSON-RPC frames (see src/lib/a2aClient.ts
// processSSEStream): frames are "\n\n"-delimited, each line prefixed "data: ", each
// payload unwrapped as `.result`. We echo the caller's own contextId/taskId (read
// from the request) so the mocked reply lines up with the real session the backend
// just created — no hard-coded session id needed.

const AGENT_REPLY = process.env.E2E_AGENT_REPLY ?? "Hello from the agent";

const frame = (event) => `data: ${JSON.stringify({ jsonrpc: "2.0", id: "1", result: event })}\n\n`;

function chatStream(contextId, taskId) {
  const message = {
    kind: "message",
    role: "agent",
    parts: [{ kind: "text", text: AGENT_REPLY }],
    contextId,
    taskId,
    messageId: 1,
    metadata: {},
  };
  const completed = {
    kind: "status-update",
    taskId,
    contextId,
    final: true,
    status: { state: "completed", message },
  };
  return frame(completed) + "data: [DONE]\n\n";
}

async function handleChat(req, res) {
  const raw = await readBody(req);
  let contextId = "e2e-session";
  let taskId = "e2e-task";
  try {
    const rpc = JSON.parse(raw);
    const msg = rpc?.params?.message ?? {};
    if (typeof msg.contextId === "string") contextId = msg.contextId;
    if (typeof msg.taskId === "string") taskId = msg.taskId;
  } catch {
    // Keep the fallback ids on a malformed body.
  }
  console.log(`[proxy] CHAT ${req.method} ${req.url} -> mocked SSE (contextId=${contextId})`);
  res.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  res.end(chatStream(contextId, taskId));
}

// endregion

// region Proxy

function forward(req, res) {
  const target = new URL(`${BACKEND_ORIGIN}${req.url}`);
  const doRequest = target.protocol === "https:" ? httpsRequest : httpRequest;
  const headers = { ...req.headers, host: target.host };
  const upstream = doRequest(target, { method: req.method, headers }, (up) => {
    console.log(`[proxy] ${req.method} ${req.url} -> ${up.statusCode} (${BACKEND_ORIGIN})`);
    res.writeHead(up.statusCode ?? 502, up.headers);
    up.pipe(res);
  });
  upstream.on("error", (err) => {
    console.error(`[proxy] ${req.method} ${req.url} -> upstream error: ${err.message}`);
    if (!res.headersSent) res.writeHead(502, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: `proxy to ${BACKEND_ORIGIN} failed: ${err.message}` }));
  });
  req.pipe(upstream);
}

// endregion

// region Server

const server = createServer((req, res) => {
  const method = req.method ?? "GET";
  const url = req.url ?? "/";
  const pathname = url.split("?")[0];

  // Health check — playwright.config.ts webServer waits on this.
  if (pathname === "/__mock/health") return json(res, 200, { status: "ok" });

  // Chat: intercept the A2A stream so tests never need a live LLM.
  if (pathname.includes("/a2a/") || pathname.includes("/a2a-sandboxes/")) {
    return handleChat(req, res);
  }

  // Everything else: forward to the real backend.
  return forward(req, res);
});

server.listen(PORT, "127.0.0.1", () => {
  console.log(`[proxy] kagent E2E proxy on http://127.0.0.1:${PORT} -> ${BACKEND_ORIGIN} (chat mocked)`);
});

// endregion
