// Backend discovery helpers. Specs use these to find a dependency that already
// exists in the cluster (a model config, a ready agent) instead of hard-coding a
// seeded resource by name — so a rename or reshuffle of the seeded set doesn't
// break the suite. Application resources use the controller's gRPC API.

import { expect } from "@playwright/test";
import { listAgents, listModelConfigs, type ModelConfigInfo } from "./grpc";

/** The first available model config, for suites that need one to attach to an agent. */
export async function firstModelConfig(): Promise<ModelConfigInfo> {
  const cfg = (await listModelConfigs())[0];
  expect(cfg, "no model config available — the suite needs at least one").toBeTruthy();
  return cfg!;
}

/** The ref ("namespace/name") of a deployment-ready agent, for the chat flow. */
export async function firstReadyAgent(): Promise<string> {
  const items = await listAgents();
  const pick = items.find((a) => a.deploymentReady && a.accepted) ?? items[0];
  expect(pick, "no agent available for the chat flow").toBeTruthy();
  return `${pick!.namespace}/${pick!.name}`;
}
