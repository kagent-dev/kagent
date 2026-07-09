// Semantic control seam over the stub's /__mock/* HTTP endpoints. Specs express
// intent (mock.noAgents()) instead of transport (POST /__mock/scenario ...), so
// if the mock mechanism ever changes, only this file does.
//
// Used via the `mock` fixture in playwright/fixtures/test.ts. Set scenarios
// BEFORE page.goto — fetch is cache:"no-store" and each test gets a fresh
// context, so a fresh navigation re-fetches and picks up the override.
//
// Mutations (POST/PUT/DELETE) run server-side too, so page.route can't see them:
// the stub captures their bodies and exposes them via capturedRequests()/
// lastRequest() for payload assertions.

import type { APIRequestContext } from "@playwright/test";
import { ok } from "./data";
import type { AgentResponse } from "@/types";

const STUB_URL = "http://127.0.0.1:8899";

type EndpointSlug =
  | "agents"
  | "models"
  | "modelconfigs"
  | "namespaces"
  | "toolservers"
  | "tools"
  | "substrate";

type MutationMethod = "POST" | "PUT" | "DELETE";

interface ScenarioOptions {
  status?: number;
  body?: unknown;
}

export interface CapturedRequest<T = unknown> {
  method: string;
  path: string;
  body: T;
}

export interface MockBackend {
  /** Clear all scenario overrides and captured requests (back to the happy path). */
  reset(): Promise<void>;

  /** Low-level GET override for an endpoint. */
  setScenario(endpoint: EndpointSlug, opts: ScenarioOptions): Promise<void>;
  /** Low-level mutation override, keyed by method + path (e.g. "POST", "/api/agents"). */
  setMutation(method: MutationMethod, path: string, opts: ScenarioOptions): Promise<void>;

  /** All captured mutation requests, in order. */
  capturedRequests(): Promise<CapturedRequest[]>;
  /** The most recent captured mutation matching method + path substring, or null. */
  lastRequest<T = unknown>(method: MutationMethod, pathIncludes: string): Promise<CapturedRequest<T> | null>;

  setAgents(agents: AgentResponse[]): Promise<void>;
  noAgents(): Promise<void>;
  agentsError(status?: number): Promise<void>;
  /** Force POST /api/agents (create) to fail. */
  agentsCreateError(status?: number): Promise<void>;

  noModelConfigs(): Promise<void>;
  modelConfigsError(status?: number): Promise<void>;

  noToolServers(): Promise<void>;
  toolServersError(status?: number): Promise<void>;
}

export function makeMock(request: APIRequestContext): MockBackend {
  const setScenario: MockBackend["setScenario"] = async (endpoint, opts) => {
    await request.post(`${STUB_URL}/__mock/scenario`, {
      data: { endpoint, status: opts.status, body: opts.body },
    });
  };

  const setMutation: MockBackend["setMutation"] = async (method, path, opts) => {
    await request.post(`${STUB_URL}/__mock/scenario`, {
      data: { method, path, status: opts.status, body: opts.body },
    });
  };

  const capturedRequests: MockBackend["capturedRequests"] = async () => {
    const res = await request.get(`${STUB_URL}/__mock/requests`);
    const json = (await res.json()) as { data: CapturedRequest[] };
    return json.data;
  };

  const errorBody = (label: string) => ({ error: `stubbed ${label} error` });

  return {
    reset: async () => {
      await request.post(`${STUB_URL}/__mock/reset`);
    },
    setScenario,
    setMutation,
    capturedRequests,

    lastRequest: async <T = unknown>(method: MutationMethod, pathIncludes: string) => {
      const all = await capturedRequests();
      const matches = all.filter((r) => r.method === method && r.path.includes(pathIncludes));
      return (matches[matches.length - 1] as CapturedRequest<T> | undefined) ?? null;
    },

    setAgents: (agents) => setScenario("agents", { body: ok(agents) }),
    noAgents: () => setScenario("agents", { body: ok([]) }),
    agentsError: (status = 500) => setScenario("agents", { status, body: errorBody("agents") }),
    agentsCreateError: (status = 500) =>
      setMutation("POST", "/api/agents", { status, body: errorBody("create-agent") }),

    noModelConfigs: () => setScenario("modelconfigs", { body: ok([]) }),
    modelConfigsError: (status = 500) =>
      setScenario("modelconfigs", { status, body: errorBody("modelconfigs") }),

    noToolServers: () => setScenario("toolservers", { body: ok([]) }),
    toolServersError: (status = 500) =>
      setScenario("toolservers", { status, body: errorBody("toolservers") }),
  };
}
