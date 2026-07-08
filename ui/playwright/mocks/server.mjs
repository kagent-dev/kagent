// Standalone stub backend for Playwright E2E.
//
// The kagent UI fetches data server-side (Next.js server actions), so the
// backend fetch happens in the Node process, not the browser — browser-level
// page.route cannot mock it. Instead we run this tiny HTTP server and point
// Next at it via BACKEND_INTERNAL_URL (see playwright.config.ts). getBackendUrl()
// in src/lib/utils.ts checks BACKEND_INTERNAL_URL first, so every /api/* call
// lands here.
//
// Dependency-free (Node built-in http only) so it runs without a TS/build step.
// Payloads mirror the shapes in src/types/index.ts; the typed spec-side builders
// live in playwright/mocks/data.ts. Keep the two in sync until Stage 1 unifies
// them behind the /__mock/scenario control endpoint.

import { createServer } from "node:http";

const PORT = Number(process.env.STUB_PORT ?? 8899);

// region Payloads (happy path)

// Success envelope used by the Go backend: { message, data }.
const ok = (data, message = "OK") => ({ message, data });

const agent = {
  id: "1",
  agent: {
    metadata: { name: "e2e-agent", namespace: "default" },
    spec: { type: "Declarative", description: "Seeded E2E agent" },
  },
  model: "gpt-4o",
  modelProvider: "OpenAI",
  modelConfigRef: "default/default-model-config",
  tools: [],
  deploymentReady: true,
  accepted: true,
};

const modelConfig = {
  ref: "default/default-model-config",
  spec: { model: "gpt-4o", provider: "OpenAI" },
};

const toolServer = {
  ref: "default/e2e-tool-server",
  groupKind: "RemoteMCPServer.kagent.dev",
  discoveredTools: [],
};

const tool = {
  id: "e2e-tool",
  server_name: "e2e-tool-server",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  deleted_at: "",
  description: "Seeded E2E tool",
  group_kind: "RemoteMCPServer.kagent.dev",
};

const substrateStatus = {
  enabled: false,
  workerPools: [],
  actorTemplates: [],
  actors: [],
  workers: [],
};

// GET route table keyed by pathname (query string stripped before lookup).
const GET_ROUTES = {
  "/api/agents": () => ok([agent], "Successfully fetched agents"),
  "/api/models": () => ok({ openai: [{ name: "gpt-4o", function_calling: true }] }),
  "/api/modelconfigs": () => ok([modelConfig]),
  "/api/namespaces": () => ok([{ name: "default", status: "Active" }], "Namespaces fetched successfully"),
  "/api/toolservers": () => ok([toolServer]),
  "/api/tools": () => ok([tool]),
  "/api/substrate/status": () => ok(substrateStatus, "Substrate status fetched"),
};

// endregion

// region Server

const json = (res, status, body) => {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Content-Length": Buffer.byteLength(payload),
  });
  res.end(payload);
};

const server = createServer((req, res) => {
  const { method, url } = req;
  const pathname = url.split("?")[0];

  // Control + health endpoints.
  if (pathname === "/__mock/health") return json(res, 200, { status: "ok" });
  if (method === "POST" && pathname === "/__mock/reset") {
    // No scenario state yet — happy path only. Wired up in Stage 1.
    return json(res, 200, { status: "reset" });
  }
  if (method === "POST" && pathname === "/__mock/scenario") {
    // Placeholder for per-test overrides (Stage 1).
    return json(res, 200, { status: "noop" });
  }

  if (method === "GET" && GET_ROUTES[pathname]) {
    console.log(`[stub] ${method} ${url} -> 200`);
    return json(res, 200, GET_ROUTES[pathname]());
  }

  // Anything unmocked is a real gap — make it loud so we notice leaks.
  console.warn(`[stub] UNHANDLED ${method} ${url} -> 404`);
  return json(res, 404, { error: `No stub for ${method} ${pathname}` });
});

server.listen(PORT, "127.0.0.1", () => {
  console.log(`[stub] kagent mock backend listening on http://127.0.0.1:${PORT}`);
});

// endregion
