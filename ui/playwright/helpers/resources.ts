// Backend discovery helpers. Specs use these to find a dependency that already
// exists in the cluster (a model config, a ready agent) instead of hard-coding a
// seeded resource by name — so a rename or reshuffle of the seeded set doesn't
// break the suite. Calls go through the same proxy the app uses.

import { expect, type APIRequestContext } from "@playwright/test";

const PROXY = "http://127.0.0.1:8899/api";

export interface ModelConfigInfo {
  ref: string; // "namespace/name"
  model: string;
  namespace: string;
}

/** The first available model config, for suites that need one to attach to an agent. */
export async function firstModelConfig(request: APIRequestContext): Promise<ModelConfigInfo> {
  const res = await request.get(`${PROXY}/modelconfigs`);
  expect(res.ok(), "GET /api/modelconfigs failed").toBeTruthy();
  const body = (await res.json()) as { data?: Array<{ ref: string; model?: string; spec?: { model?: string } }> };
  const cfg = (body.data ?? [])[0];
  expect(cfg, "no model config available — the suite needs at least one").toBeTruthy();
  const model = cfg!.spec?.model ?? cfg!.model ?? "";
  return { ref: cfg!.ref, model, namespace: cfg!.ref.split("/")[0] };
}

/** The ref ("namespace/name") of a deployment-ready agent, for the chat flow. */
export async function firstReadyAgent(request: APIRequestContext): Promise<string> {
  const res = await request.get(`${PROXY}/agents`);
  expect(res.ok(), "GET /api/agents failed").toBeTruthy();
  const body = (await res.json()) as {
    data?: Array<{ deploymentReady?: boolean; accepted?: boolean; agent: { metadata: { namespace: string; name: string } } }>;
  };
  const items = body.data ?? [];
  const pick = items.find((a) => a.deploymentReady && a.accepted) ?? items[0];
  expect(pick, "no agent available for the chat flow").toBeTruthy();
  const m = pick!.agent.metadata;
  return `${m.namespace}/${m.name}`;
}
