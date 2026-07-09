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
// live in playwright/mocks/data.ts and the semantic control seam in control.ts.
//
// Scenarios: default is the happy path below. A test can override any endpoint
// via POST /__mock/scenario { endpoint, status?, body? } (used by control.ts to
// force empty/error/custom responses) and clear all overrides via POST
// /__mock/reset. State is a single in-memory map — correct only while the runner
// is serial (workers: 1); see playwright/README.md.

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

// Returned by POST /api/sessions (createSession). The fixed id is what the chat
// UI uses for the new session and the streamed A2A contextId.
const session = {
  id: "e2e-session",
  name: "e2e chat",
  agent_id: "default/e2e-agent",
  user_id: "admin@kagent.dev",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  deleted_at: "",
};

// Prior conversation returned by GET /api/sessions/<id>/tasks (existing-chat load).
// Agent messages need metadata.displaySource:"assistant" to render; state must not
// be submitted/working (that would trigger stream resubscribe).
const task = {
  id: "e2e-task",
  contextId: "e2e-session",
  kind: "task",
  status: { state: "completed" },
  history: [
    { kind: "message", messageId: "t1-u", role: "user", parts: [{ kind: "text", text: "Prior question" }], metadata: { timestamp: 1 } },
    { kind: "message", messageId: "t1-a", role: "agent", parts: [{ kind: "text", text: "Prior answer" }], metadata: { displaySource: "assistant", timestamp: 2 } },
  ],
};

// Default happy-path body per endpoint slug.
const DEFAULTS = {
  agents: () => ok([agent], "Successfully fetched agents"),
  // The provider→models map is keyed by the capitalized provider name ("OpenAI"),
  // matching v1alpha2.ModelProviderOpenAI — the Model dropdown indexes on it.
  models: () => ok({ OpenAI: [{ name: "gpt-4o", function_calling: true }] }),
  modelconfigs: () => ok([modelConfig]),
  namespaces: () => ok([{ name: "default", status: "Active" }], "Namespaces fetched successfully"),
  toolservers: () => ok([toolServer]),
  tools: () => ok([tool]),
  substrate: () => ok(substrateStatus, "Substrate status fetched"),
  // Stock model providers (getSupportedModelProviders) — enables the model form.
  providers: () => ok([{ name: "OpenAI", type: "OpenAI", requiredParams: [], optionalParams: [] }]),
  configuredProviders: () => ok([]),
  // Tool server types (getToolServerTypes) — blocking on /mcp/new until it loads.
  toolservertypes: () => ok(["RemoteMCPServer", "MCPServer"]),
  // Prompt libraries list (listPromptTemplates?namespace=<ns>).
  prompttemplates: () => ok([]),
};

// GET pathname -> endpoint slug (query string stripped before lookup).
const PATH_TO_SLUG = {
  "/api/agents": "agents",
  "/api/models": "models",
  "/api/modelconfigs": "modelconfigs",
  "/api/namespaces": "namespaces",
  "/api/toolservers": "toolservers",
  "/api/tools": "tools",
  "/api/substrate/status": "substrate",
  "/api/modelproviderconfigs/models": "providers",
  "/api/modelproviderconfigs/configured": "configuredProviders",
  "/api/toolservertypes": "toolservertypes",
  "/api/prompttemplates": "prompttemplates",
};

// endregion

// region Scenario state

// Overrides set by POST /__mock/scenario. Empty = happy path.
//   - GET reads keyed by endpoint slug ("agents", "models", …)
//   - mutations keyed by "<METHOD> <pathname>" (e.g. "POST /api/agents")
let overrides = {};

// Captured mutation requests (POST/PUT/DELETE) so specs can assert payloads —
// the app fetches server-side, so page.route can't see them. Read via
// GET /__mock/requests; cleared by /__mock/reset.
let requests = [];

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

const readJsonBody = (req) =>
  new Promise((resolve) => {
    let raw = "";
    req.on("data", (chunk) => {
      raw += chunk;
    });
    req.on("end", () => {
      if (!raw) return resolve({});
      try {
        resolve(JSON.parse(raw));
      } catch {
        resolve(null);
      }
    });
    req.on("error", () => resolve(null));
  });

const server = createServer(async (req, res) => {
  // req.method/url are typed string|undefined; default them so a malformed
  // request can't throw on the split below.
  const method = req.method ?? "GET";
  const url = req.url ?? "/";
  const pathname = url.split("?")[0];

  // Control + health endpoints.
  if (pathname === "/__mock/health") return json(res, 200, { status: "ok" });

  if (method === "GET" && pathname === "/__mock/requests") {
    return json(res, 200, { data: requests });
  }

  if (method === "POST" && pathname === "/__mock/reset") {
    overrides = {};
    requests = [];
    return json(res, 200, { status: "reset" });
  }

  if (method === "POST" && pathname === "/__mock/scenario") {
    const body = await readJsonBody(req);
    // Mutation override: { method, path, status?, body? } keyed by "<METHOD> <path>".
    if (body && typeof body.method === "string" && typeof body.path === "string") {
      const key = `${body.method} ${body.path}`;
      overrides[key] = { status: body.status ?? 200, body: body.body };
      console.log(`[stub] scenario set: ${key} -> ${body.status ?? 200}`);
      return json(res, 200, { status: "scenario-set", key });
    }
    // GET override: { endpoint, status?, body? } keyed by endpoint slug.
    if (body && typeof body.endpoint === "string") {
      overrides[body.endpoint] = { status: body.status ?? 200, body: body.body };
      console.log(`[stub] scenario set: ${body.endpoint} -> ${body.status ?? 200}`);
      return json(res, 200, { status: "scenario-set", endpoint: body.endpoint });
    }
    return json(res, 400, { error: "scenario requires { endpoint } or { method, path }" });
  }

  if (method === "GET") {
    const slug = PATH_TO_SLUG[pathname];
    if (slug) {
      const override = overrides[slug];
      if (override) {
        console.log(`[stub] ${method} ${url} -> ${override.status} (override)`);
        return json(res, override.status, override.body ?? {});
      }
      console.log(`[stub] ${method} ${url} -> 200`);
      return json(res, 200, DEFAULTS[slug]());
    }

    // Dynamic GET routes (parameterized paths not in PATH_TO_SLUG).
    if (/^\/api\/agents\/[^/]+\/[^/]+$/.test(pathname)) {
      console.log(`[stub] ${method} ${url} -> 200 (agent detail)`);
      return json(res, 200, ok(agent));
    }
    if (/^\/api\/sessions\/agent\/[^/]+\/[^/]+$/.test(pathname)) {
      console.log(`[stub] ${method} ${url} -> 200 (sessions for agent)`);
      return json(res, 200, ok([]));
    }
    // Session tasks (existing-chat history): /api/sessions/<id>/tasks.
    if (/^\/api\/sessions\/[^/]+\/tasks$/.test(pathname)) {
      console.log(`[stub] ${method} ${url} -> 200 (session tasks)`);
      return json(res, 200, ok([task]));
    }
    // Single session (checkSessionExists): truthy for the seeded id, else 404 so
    // the "Session not found" screen is reachable.
    const sessionDetail = pathname.match(/^\/api\/sessions\/([^/]+)$/);
    if (sessionDetail) {
      if (sessionDetail[1] === "e2e-session") {
        console.log(`[stub] ${method} ${url} -> 200 (session detail)`);
        return json(res, 200, ok({ session }));
      }
      console.log(`[stub] ${method} ${url} -> 404 (session not found)`);
      return json(res, 404, { error: "Session not found" });
    }
    // Single model config (edit page load): /api/modelconfigs/<ns>/<name>.
    if (/^\/api\/modelconfigs\/[^/]+\/[^/]+$/.test(pathname)) {
      console.log(`[stub] ${method} ${url} -> 200 (model config detail)`);
      return json(res, 200, ok(modelConfig));
    }
    // Prompt library detail (redirect target after create): /api/prompttemplates/<ns>/<name>.
    const promptDetail = pathname.match(/^\/api\/prompttemplates\/([^/]+)\/([^/]+)$/);
    if (promptDetail) {
      console.log(`[stub] ${method} ${url} -> 200 (prompt template detail)`);
      return json(res, 200, ok({ namespace: promptDetail[1], name: promptDetail[2], data: {} }));
    }
  }

  // Mutations to /api/*: capture the body (for payload assertions) and respond.
  // Default is 200 echoing the sent body in the success envelope; an override can
  // force an error (e.g. 500) for failure-path tests.
  if ((method === "POST" || method === "PUT" || method === "DELETE") && pathname.startsWith("/api/")) {
    const body = await readJsonBody(req);
    requests.push({ method, path: url, body });
    const override = overrides[`${method} ${pathname}`];
    if (override) {
      console.log(`[stub] ${method} ${url} -> ${override.status} (override)`);
      return json(res, override.status, override.body ?? {});
    }
    // createSession must return a session WITH an id (the UI uses it for the new
    // chat's id + streamed contextId); the generic echo below wouldn't have one.
    if (method === "POST" && pathname === "/api/sessions") {
      console.log(`[stub] ${method} ${url} -> 200 (session created)`);
      return json(res, 200, ok(session));
    }
    // createAgentHarnessFromForm reads response.data.agent — echo needs .agent.
    if (method === "POST" && pathname === "/api/agentharnesses") {
      console.log(`[stub] ${method} ${url} -> 200 (harness created)`);
      return json(res, 200, ok(agent));
    }
    console.log(`[stub] ${method} ${url} -> 200 (captured)`);
    return json(res, 200, ok(body));
  }

  // Anything unmocked is a real gap — make it loud so we notice leaks.
  console.warn(`[stub] UNHANDLED ${method} ${url} -> 404`);
  return json(res, 404, { error: `No stub for ${method} ${pathname}` });
});

server.listen(PORT, "127.0.0.1", () => {
  console.log(`[stub] kagent mock backend listening on http://127.0.0.1:${PORT}`);
});

// endregion
