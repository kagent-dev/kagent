/** Reusable Harness and AgentTemplate resources used to define Agents. */

import { namespaceReadError } from "./namespaceReadError";
import { apiClient } from "../client";
import { type AgentTemplate } from "../domain/agentTemplates";
import type { Harness } from "../domain/harnesses";
import { type ApiResource, useApiResource } from "./useApiResource";
import { useAgent } from "./useAgents";

/**
 * The harnesses in one namespace, or in every observed namespace.
 *
 * Unlike agent instances, `HarnessService` reads across namespaces when given an
 * empty one — it is a Kubernetes list, not a per-namespace database query — so
 * "all namespaces" is one request rather than a fan-out.
 */
export function useHarnesses(namespace?: string): ApiResource<Harness[]> {
  return useApiResource(["harnesses.list", namespace ?? ""], () =>
    apiClient.agentBuildingBlocks.harnesses(namespace),
  );
}

/** Whether an Agent (`namespace/name`) takes files; undefined until known. Only the kagent harness does. */
export function useAgentTakesFiles(ref?: string): boolean | undefined {
  const [namespace, name] = ref?.split("/") ?? [];
  const spec = useAgent(namespace, name).data?.resource.spec;
  const refName = spec?.harnessRef.name;
  const harnesses = useApiResource(refName ? ["harnesses.list", namespace] : null, () =>
    apiClient.agentBuildingBlocks.harnesses(namespace),
  );
  const harness = harnesses.data?.find((candidate) => candidate.ref === `${namespace}/${refName}`);
  return harness && harness.runtime === "kagent";
}

/** The agent templates in one namespace, or in every observed namespace. */
export function useAgentTemplates(namespace?: string): ApiResource<AgentTemplate[]> {
  return useApiResource(["agentTemplates.list", namespace ?? ""], () =>
    apiClient.agentBuildingBlocks.agentTemplates(namespace),
  );
}

/** Reads the named namespaces, or every visible namespace when passed null. */
export function useHarnessesAcrossNamespaces(
  namespaces: readonly string[] | null | undefined,
): ApiResource<Harness[]> {
  const key = namespaces === null ? "*" : namespaces?.slice().sort().join(",");

  return useApiResource(key ? ["harnesses.listAll", key] : null, async () => {
    const names = key === "*" ? [""] : key ? key.split(",") : [];
    const settled = await Promise.allSettled(
      names.map((namespace) => apiClient.agentBuildingBlocks.harnesses(namespace)),
    );

    const harnesses: Harness[] = [];
    const refused = settled.filter((result) => result.status === "rejected");
    if (names.length > 0 && refused.length === names.length) {
      throw namespaceReadError(refused);
    }
    for (const outcome of settled) {
      if (outcome.status === "fulfilled") harnesses.push(...outcome.value);
    }
    return harnesses;
  });
}

/** Templates and the namespaces that refused, when reading more than one. */
export interface AgentTemplatesAcrossNamespaces {
  templates: AgentTemplate[];
  refused: { namespace: string; reason: string }[];
}

/**
 * Reads the named namespaces, or every visible namespace when passed null.
 * Returns partial refusals alongside any successfully loaded templates.
 */
export function useAgentTemplatesAcrossNamespaces(
  namespaces: readonly string[] | null | undefined,
): ApiResource<AgentTemplatesAcrossNamespaces> {
  // Sorted into the key, so the same set in a different order is the same read rather
  // than a cache miss that refetches everything.
  const key = namespaces === null ? "*" : namespaces?.slice().sort().join(",");

  return useApiResource(
    key ? ["agentTemplates.listAll", key] : null,
    async () => {
      const names = key === "*" ? [""] : key ? key.split(",") : [];
      const settled = await Promise.allSettled(
        names.map((namespace) => apiClient.agentBuildingBlocks.agentTemplates(namespace)),
      );

      const templates: AgentTemplate[] = [];
      const refused: { namespace: string; reason: string }[] = [];
      settled.forEach((outcome, index) => {
        if (outcome.status === "fulfilled") {
          templates.push(...outcome.value);
          return;
        }
        const cause = outcome.reason;
        refused.push({
          namespace: names[index],
          reason: cause instanceof Error ? cause.message : String(cause),
        });
      });

      /*
       * Every namespace refusing is a failed read, not an empty cluster.
       *
       * `allSettled` turns a total failure into `{templates: [], refused: [...]}`,
       * which a page renders as "no agents" — the empty state lying about a backend
       * that answered nothing. Partial refusal is a real and useful state; total
       * refusal is the page being broken, and it has to reach the caller as an error
       * so the failure is reported rather than drawn as an absence.
       */
      if (names.length > 0 && refused.length === names.length) {
        throw namespaceReadError(settled.filter((result) => result.status === "rejected"));
      }

      templates.sort(
        (a, b) => a.namespace.localeCompare(b.namespace) || a.name.localeCompare(b.name),
      );
      return { templates, refused };
    },
  );
}

/** One agent template, whole. Held back until the route has given us its address. */
export function useAgentTemplate(
  namespace: string | undefined,
  name: string | undefined,
): ApiResource<AgentTemplate> {
  return useApiResource(
    namespace && name ? ["agentTemplates.get", namespace, name] : null,
    () => apiClient.agentBuildingBlocks.agentTemplate(namespace ?? "", name ?? ""),
  );
}
