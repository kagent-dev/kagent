import { test, expect } from "../fixtures/test";
import {
  deleteAgent,
  deleteModelConfig,
  deletePromptTemplate,
  deleteToolServer,
  listAgents,
  listModelConfigs,
  listPromptTemplateRefs,
  listToolServerRefs,
} from "../helpers/grpc";

// Housekeeping — delete any leftover e2e-* resources. Every other suite already
// deletes what it creates on a green run, so this only matters when a run crashed
// mid-way (leaving a uniquely-named resource behind). It sweeps the resource CRDs
// by name prefix via the controller's gRPC API; it asserts nothing about product
// behaviour.
//
// Only names starting with this prefix are touched, so seeded resources
// (k8s-agent, default-model-config, …) are never at risk.
const PREFIX = "e2e-";

const isTestRef = (ref: string | null): ref is string => !!ref && (ref.split("/")[1] ?? "").startsWith(PREFIX);

test("cleanup: remove leftover e2e resources", async () => {
  // region Reading — collect leftover e2e-* refs across resource types
  const [agents, toolServers, modelConfigs, prompts] = await Promise.all([
    listAgents(),
    listToolServerRefs(),
    listModelConfigs(),
    listPromptTemplateRefs("kagent"),
  ]);

  // region Deleting — delete each leftover
  for (const agent of agents.filter((item) => isTestRef(`${item.namespace}/${item.name}`))) {
    await deleteAgent(agent);
  }
  for (const ref of toolServers.filter(isTestRef)) {
    await deleteToolServer(ref);
  }
  for (const config of modelConfigs.filter((item) => isTestRef(item.ref))) {
    await deleteModelConfig(config.ref);
  }
  for (const ref of prompts.filter(isTestRef)) {
    await deletePromptTemplate(ref);
  }

  // Best-effort housekeeping — the exact leftovers vary run to run, so there's
  // nothing meaningful to assert beyond "the sweep ran".
  expect(test.info().errors).toEqual([]);
});
