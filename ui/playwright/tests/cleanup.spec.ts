import { test, expect } from "../fixtures/test";
import type { APIRequestContext } from "@playwright/test";

// Housekeeping — delete any leftover e2e-* resources. Every other suite already
// deletes what it creates on a green run, so this only matters when a run crashed
// mid-way (leaving a uniquely-named resource behind). It sweeps the resource CRDs
// by name prefix via the same proxy the app uses (which forwards to the real
// backend); it asserts nothing about product behaviour.
//
// Only names starting with this prefix are touched, so seeded resources
// (k8s-agent, default-model-config, …) are never at risk.
const PREFIX = "e2e-";
const PROXY = "http://127.0.0.1:8899/api";

const isTestRef = (ref: string | null): ref is string => !!ref && (ref.split("/")[1] ?? "").startsWith(PREFIX);

async function listRefs(
  request: APIRequestContext,
  path: string,
  toRef: (item: Record<string, unknown>) => string | null,
): Promise<string[]> {
  const res = await request.get(`${PROXY}/${path}`);
  if (!res.ok()) return [];
  const body = (await res.json()) as { data?: Record<string, unknown>[] };
  return (body.data ?? []).map(toRef).filter(isTestRef);
}

test("cleanup: remove leftover e2e-* test resources", async ({ request }) => {
  const agents = await listRefs(request, "agents", (a) => {
    const m = (a.agent as { metadata?: { namespace?: string; name?: string } })?.metadata;
    return m?.namespace && m?.name ? `${m.namespace}/${m.name}` : null;
  });
  const toolServers = await listRefs(request, "toolservers", (t) => (t.ref as string) ?? null);
  const modelConfigs = await listRefs(request, "modelconfigs", (m) => (m.ref as string) ?? null);
  const prompts = await listRefs(request, "prompttemplates?namespace=kagent", (p) =>
    p.namespace && p.name ? `${p.namespace}/${p.name}` : null,
  );

  const sweep = async (path: string, refs: string[]) => {
    for (const ref of refs) {
      const res = await request.delete(`${PROXY}/${path}/${ref}`);
      console.log(`[cleanup] DELETE ${path}/${ref} -> ${res.status()}`);
    }
  };

  await sweep("agents", agents);
  await sweep("toolservers", toolServers);
  await sweep("modelconfigs", modelConfigs);
  await sweep("prompttemplates", prompts);

  // Best-effort housekeeping — the exact leftovers vary run to run, so there's
  // nothing meaningful to assert beyond "the sweep ran".
  expect(test.info().errors).toEqual([]);
});
