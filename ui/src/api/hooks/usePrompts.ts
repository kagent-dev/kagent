import { apiClient } from "../client";
import type { PromptTemplateDetail, PromptTemplateSummary } from "../domain/prompts";
import { sortedByFields } from "../order";
import { type ApiResource, useApiResource } from "./useApiResource";

/**
 * Prompt libraries, optionally narrowed to a set of namespaces.
 *
 * "All namespaces" is a fan-out, not a single call. `ListPromptTemplates` takes one
 * namespace and rejects a request without one — `namespace query parameter is
 * required` — so the one request the page used to make for its own default filter
 * could never succeed against a real controller. The fixtures answered it happily,
 * which is why the page looked fine until it met a cluster.
 *
 * That constraint is also what makes this page's namespace filter genuinely
 * server-side, unlike the models and MCP server lists: the request carries a
 * namespace, so asking for two namespaces is two reads of exactly those two rather
 * than a read of everything narrowed afterwards. Nothing here fetches a namespace the
 * reader did not ask for.
 *
 * Passing nothing keeps the old behaviour — every namespace the app can see, which is
 * the same list the filter offers, so this asks for exactly the namespaces a reader
 * could have picked.
 */
export function usePrompts(
  namespaces?: readonly string[],
): ApiResource<PromptTemplateSummary[]> {
  // Sorted into the cache key so that picking the same two namespaces in the other
  // order is the same read rather than a second one.
  const wanted = namespaces && namespaces.length > 0 ? [...namespaces].sort() : undefined;

  return useApiResource(["prompts.list", wanted?.join(",") ?? "*"], async () => {
    const scope =
      wanted ?? (await apiClient.namespaces.list()).map((entry) => entry.name);

    const perNamespace = await Promise.all(
      scope.map((namespace) => apiClient.prompts.list(namespace)),
    );
    // Deliberately not catching a per-namespace failure. One namespace failing means
    // the list on screen is incomplete, and quietly showing a shorter list is the
    // failure mode this codebase avoids everywhere else.
    //
    // Sorted again after the merge: each call sorted the rows it returned, and
    // concatenating sorted runs does not give a sorted whole.
    return sortedByFields(perNamespace.flat());
  });
}

/** One prompt library and its fragments. Holds off until the ref is known. */
export function usePrompt(
  namespace: string | undefined,
  name: string | undefined,
): ApiResource<PromptTemplateDetail> {
  return useApiResource(
    namespace && name ? ["prompts.get", namespace, name] : null,
    () => apiClient.prompts.get(namespace!, name!),
  );
}
