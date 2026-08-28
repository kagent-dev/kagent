/**
 * The two halves an agent is made of, for the pickers that choose them.
 *
 * A `Harness` says how an agent runs; an `AgentTemplate` says what it is.
 * `CreateAgentInstance` names one of each, so creating an agent is choosing a
 * pair — which is why these are read together and why the create form is two
 * pickers rather than a spec form.
 */

import { apiClient } from "../client";
import { admitsHarness, type AgentTemplate } from "../domain/agentTemplates";
import type { Harness } from "../domain/harnesses";
import { type ApiResource, useApiResource } from "./useApiResource";

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

/** The agent templates in one namespace, or in every observed namespace. */
export function useAgentTemplates(namespace?: string): ApiResource<AgentTemplate[]> {
  return useApiResource(["agentTemplates.list", namespace ?? ""], () =>
    apiClient.agentBuildingBlocks.agentTemplates(namespace),
  );
}

/**
 * The harnesses in every namespace named, read one namespace at a time.
 *
 * `ListHarnesses` validates its namespace and refuses an empty one rather than treating
 * it as a wildcard — the same rule `ListAgentTemplates` has, and the same trap: a page
 * asking for "all harnesses" with no namespace gets a refusal, and the fixtures answer
 * it happily so nothing says otherwise until a cluster does.
 *
 * Partial refusal is a real state and is reported as the empty half of the pair rather
 * than as a failure. Total refusal is a broken read and throws, because `allSettled`
 * would otherwise turn it into an empty list — a page saying "no harnesses" about a
 * backend that answered nothing.
 */
export function useHarnessesAcrossNamespaces(
  namespaces: readonly string[] | undefined,
): ApiResource<Harness[]> {
  const key = namespaces ? [...namespaces].sort().join(",") : undefined;

  return useApiResource(key ? ["harnesses.listAll", key] : null, async () => {
    const names = key ? key.split(",").filter(Boolean) : [];
    const settled = await Promise.allSettled(
      names.map((namespace) => apiClient.agentBuildingBlocks.harnesses(namespace)),
    );

    const harnesses: Harness[] = [];
    const refused: string[] = [];
    settled.forEach((outcome, index) => {
      if (outcome.status === "fulfilled") harnesses.push(...outcome.value);
      else
        refused.push(
          `${names[index]}: ${
            outcome.reason instanceof Error ? outcome.reason.message : String(outcome.reason)
          }`,
        );
    });

    if (names.length > 0 && refused.length === names.length) {
      throw new Error(refused.join("; "));
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
 * The agent templates in every namespace named, read one namespace at a time.
 *
 * `AgentTemplateService.List` validates its namespace before anything else and answers
 * `InvalidArgument: namespace is required` for an empty one — it is not a wildcard. So
 * "all namespaces" is a request per namespace, merged, exactly as
 * `useAgentInstancesAcrossNamespaces` does for the same reason.
 *
 * **This is why the agents page cannot simply call `useAgentTemplates()` unscoped.** It
 * did, and against a real controller the whole page failed with "namespace is required"
 * while the fixture backend served it happily — the mock accepted an empty namespace
 * that the controller never would. That is the fixture-more-permissive-than-the-backend
 * trap this repository keeps recording, and it is why the page is read against a cluster
 * before it is called done.
 *
 * Partial rather than all-or-nothing: a reader may be allowed six namespaces and refused
 * a seventh, and failing the whole list because of one would show them nothing. The
 * refusals are returned so the page can name them instead of quietly shortening the list.
 */
export function useAgentTemplatesAcrossNamespaces(
  namespaces: readonly string[] | undefined,
): ApiResource<AgentTemplatesAcrossNamespaces> {
  // Sorted into the key, so the same set in a different order is the same read rather
  // than a cache miss that refetches everything.
  const key = namespaces ? [...namespaces].sort().join(",") : undefined;

  return useApiResource(
    key ? ["agentTemplates.listAll", key] : null,
    async () => {
      const names = key ? key.split(",").filter(Boolean) : [];
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
        throw new Error(refused.map((entry) => entry.reason).join("; "));
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

/**
 * The templates a given harness will accept, and the ones it will not.
 *
 * Both halves, because a picker that silently showed only the admitted templates
 * would leave a reader who cannot find theirs with nothing to read: the reason a
 * template is missing is that no harness admits it yet, and that is a sentence
 * worth putting on screen rather than an absence to puzzle over.
 *
 * `harnessName` is the bare name. A harness admits templates in its own namespace
 * only, so `admitting_harnesses` carries names and comparing full refs would never
 * match.
 */
export function partitionByAdmission(
  templates: readonly AgentTemplate[],
  harnessName: string | undefined,
): { admitted: AgentTemplate[]; rejected: AgentTemplate[] } {
  // With no harness chosen there is nothing to admit against, so everything is
  // still on offer rather than nothing being.
  if (!harnessName) return { admitted: [...templates], rejected: [] };

  const admitted: AgentTemplate[] = [];
  const rejected: AgentTemplate[] = [];
  for (const template of templates) {
    if (admitsHarness(template, harnessName)) admitted.push(template);
    else rejected.push(template);
  }
  return { admitted, rejected };
}
