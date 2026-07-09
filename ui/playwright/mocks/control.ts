// Semantic control seam over the stub's /__mock/* HTTP endpoints. Specs express
// intent (mock.noAgents()) instead of transport (POST /__mock/scenario ...), so
// if the mock mechanism ever changes, only this file does.
//
// Used via the `mock` fixture in playwright/fixtures/test.ts. Set scenarios
// BEFORE page.goto — fetch is cache:"no-store" and each test gets a fresh
// context, so a fresh navigation re-fetches and picks up the override.

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

interface ScenarioOptions {
  status?: number;
  body?: unknown;
}

export interface MockBackend {
  /** Clear all scenario overrides (back to the happy path). */
  reset(): Promise<void>;
  /** Low-level override for any endpoint. */
  setScenario(endpoint: EndpointSlug, opts: ScenarioOptions): Promise<void>;

  setAgents(agents: AgentResponse[]): Promise<void>;
  noAgents(): Promise<void>;
  agentsError(status?: number): Promise<void>;

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

  const errorBody = (endpoint: EndpointSlug) => ({ error: `stubbed ${endpoint} error` });

  return {
    reset: async () => {
      await request.post(`${STUB_URL}/__mock/reset`);
    },
    setScenario,

    setAgents: (agents) => setScenario("agents", { body: ok(agents) }),
    noAgents: () => setScenario("agents", { body: ok([]) }),
    agentsError: (status = 500) => setScenario("agents", { status, body: errorBody("agents") }),

    noModelConfigs: () => setScenario("modelconfigs", { body: ok([]) }),
    modelConfigsError: (status = 500) =>
      setScenario("modelconfigs", { status, body: errorBody("modelconfigs") }),

    noToolServers: () => setScenario("toolservers", { body: ok([]) }),
    toolServersError: (status = 500) =>
      setScenario("toolservers", { status, body: errorBody("toolservers") }),
  };
}
