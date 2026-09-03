import { apiClient } from "../client";
import type { AgentInstance } from "../domain/agentInstances";
import { type ApiResource, useApiResource } from "./useApiResource";

/**
 * The agent instances in one namespace.
 *
 * The namespace is a required argument and not a filter: `AgentInstanceService`
 * has no cross-namespace read, and asking with an empty one is an
 * `InvalidArgument` rather than a wider search. A caller that does not have a
 * namespace yet — a page waiting on its list of them — passes `undefined` and the
 * read is held back, which `useApiResource` reports as idle rather than loading.
 *
 * `allCreators` is part of the key, so turning it on refetches rather than
 * re-rendering the previous answer. It can be refused on its own: the controller
 * authorises "other people's instances" separately from the list, so the same
 * namespace can succeed without it and fail with it.
 */
export function useAgentInstances(
  namespace: string | undefined,
  allCreators = false,
): ApiResource<AgentInstance[]> {
  return useApiResource(
    namespace ? ["agentInstances.list", namespace, allCreators] : null,
    () => apiClient.agentInstances.list(namespace ?? "", { allCreators }),
  );
}

/**
 * What a read across several namespaces found, and what it could not.
 *
 * Partial by design. Each namespace is authorised on its own — and `allCreators` is
 * authorised separately again — so a reader can be allowed six namespaces and refused
 * the seventh. Reporting only the instances would quietly show a shorter list than the
 * cluster holds; failing the whole read because one namespace was refused would show
 * nothing at all. So both halves come back and the page says so.
 */
export interface AgentInstancesAcrossNamespaces {
  instances: AgentInstance[];
  /** The namespaces that could not be read, with the reason each gave. */
  refused: { namespace: string; reason: string }[];
}

/**
 * The agent instances in every namespace named, read one namespace at a time.
 *
 * `AgentInstanceService` has no cross-namespace read: `List` validates its namespace as
 * a DNS-1123 label before anything else, so an empty one is an `InvalidArgument` rather
 * than a wildcard. "All namespaces" is therefore this — a request per namespace, run
 * together and merged — and it costs one round trip per namespace, which is why the
 * page offers it as a choice rather than doing it invisibly.
 *
 * Ordered by namespace and then by creation, so a merged list reads as a list rather
 * than as whichever namespace answered first.
 */
export function useAgentInstancesAcrossNamespaces(
  namespaces: readonly string[] | undefined,
  allCreators = false,
): ApiResource<AgentInstancesAcrossNamespaces> {
  // Sorted into the key so the same set of namespaces in a different order is the same
  // read, rather than a cache miss that refetches everything.
  const key = namespaces ? [...namespaces].sort().join(",") : undefined;

  return useApiResource(
    key ? ["agentInstances.listAll", key, allCreators] : null,
    async () => {
      const names = key ? key.split(",").filter(Boolean) : [];
      const settled = await Promise.allSettled(
        names.map((namespace) => apiClient.agentInstances.list(namespace, { allCreators })),
      );

      const instances: AgentInstance[] = [];
      const refused: { namespace: string; reason: string }[] = [];
      settled.forEach((outcome, index) => {
        if (outcome.status === "fulfilled") {
          instances.push(...outcome.value);
          return;
        }
        const cause = outcome.reason;
        refused.push({
          namespace: names[index],
          reason: cause instanceof Error ? cause.message : String(cause),
        });
      });

      instances.sort(
        (a, b) =>
          a.namespace.localeCompare(b.namespace) ||
          (b.createdAt ?? "").localeCompare(a.createdAt ?? ""),
      );
      return { instances, refused };
    },
  );
}

/**
 * One agent's conversations, and which of them this reader can actually open.
 *
 * Two lists, because those are two different questions and only the controller can
 * answer the second one. `ListAgentInstances` with `all_creators` returns every
 * conversation with this agent; without it, exactly the caller's own. Comparing the
 * two is how this page knows which rows are openable — and it is the controller's
 * own answer rather than a guess about who is signed in, which matters because the
 * app is served behind several different authentication arrangements and one of them
 * is none at all.
 *
 * It has to be asked, because the answer is surprising: `GetAgentInstance` resolves
 * through `GetAgentInstanceForUser` — `WHERE namespace = $1 AND id = $2 AND user_id
 * = $3` — and the A2A gateway reads the instance through the same call. So somebody
 * else's conversation is listable and *not openable*: both its record page and its
 * chat answer `NotFound`. Listing it as if it opened would be worse than not listing
 * it, so the page needs to know which is which.
 */
export interface AgentConversations {
  /** Every conversation with this agent the caller was allowed to see. */
  all: AgentInstance[];
  /** The ids the caller created, which are the ones that will open. */
  openableIds: Set<string>;
  /**
   * Why the list is only the caller's own, when it is.
   *
   * `all_creators` is authorised separately from the list and the controller
   * *refuses* the request when it is not allowed rather than quietly narrowing it —
   * checked in `Service.List`, which returns the authorisation error. So a reader
   * without that permission gets an error where they wanted a list, and answering
   * with their own conversations plus this sentence is better than answering with
   * nothing. Undefined when the wide read succeeded.
   */
  widerReadRefused?: string;
}

/**
 * The conversations with one agent — a `(AgentTemplate, Harness)` pair.
 *
 * Narrowed by the server: `ListAgentInstances` takes `agent_template` and `harness`
 * and resolves them through the prepared revision, so the filtering happens before
 * the page is cut. Filtering in the browser would search one page of a paged read
 * and report "no conversations" about a row further down.
 *
 * Held back until it has all three parts of the address, which `useApiResource`
 * reports as idle rather than as loading.
 */
export function useAgentConversations(
  namespace: string | undefined,
  agentTemplate: string | undefined,
  harness: string | undefined,
): ApiResource<AgentConversations> {
  return useApiResource(
    namespace && agentTemplate && harness
      ? ["agentInstances.forAgent", namespace, agentTemplate, harness]
      : null,
    async () => {
      const scope = {
        agentTemplate: agentTemplate ?? "",
        harness: harness ?? "",
      };
      /*
       * The narrow read is the one that must succeed, so it is awaited on its own
       * terms: it decides which rows open, and a failure there is a failure of the
       * page. The wide one is allowed to be refused.
       */
      const [wide, own] = await Promise.all([
        apiClient.agentInstances
          .list(namespace ?? "", { ...scope, allCreators: true })
          .then(
            (rows) => ({ rows, refused: undefined as string | undefined }),
            (cause: unknown) => ({
              rows: undefined,
              refused: cause instanceof Error ? cause.message : String(cause),
            }),
          ),
        apiClient.agentInstances.list(namespace ?? "", scope),
      ]);

      return {
        all: wide.rows ?? own,
        openableIds: new Set(own.map((row) => row.id)),
        widerReadRefused: wide.refused,
      };
    },
  );
}

/** One agent instance. Held back until the route has given us both halves of its address. */
export function useAgentInstance(
  namespace: string | undefined,
  id: string | undefined,
): ApiResource<AgentInstance> {
  return useApiResource(
    namespace && id ? ["agentInstances.get", namespace, id] : null,
    () => apiClient.agentInstances.get(namespace ?? "", id ?? ""),
  );
}
