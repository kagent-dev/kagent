/**
 * The order every list of resources arrives in.
 *
 * Sorted here, at the client boundary, rather than in each table. A list's order
 * is a property of the collection and not of the screen showing it, and the same
 * collections are read by more than one screen apiece — the agents list feeds the
 * agents page, the dashboard and the substrate page, and a reader moving between
 * them should not have to re-find their bearings each time. Doing it per table also
 * means the next table added quietly gets whatever order the backend felt like.
 *
 * Namespace first, then name, both descending — the order asked for. The controller
 * returns rows in whatever order its informer cache yields, which is stable within
 * a process and not across restarts, so without this a page's rows can rearrange
 * for no reason a reader can see.
 */

/** A resource's identity, however the response happened to carry it. */
export interface ResourceName {
  namespace: string;
  name: string;
}

/**
 * Splits the `namespace/name` refs several of these responses use.
 *
 * Splits at the *first* slash, so a name containing one keeps it rather than the
 * remainder being dropped. A ref with no slash is a bare name in no stated
 * namespace, which sorts as an empty namespace rather than being discarded.
 */
export function parseResourceRef(ref: string): ResourceName {
  const slash = ref.indexOf("/");
  return slash === -1
    ? { namespace: "", name: ref }
    : { namespace: ref.slice(0, slash), name: ref.slice(slash + 1) };
}

/**
 * Compares two identities: namespace descending, then name descending.
 *
 * `localeCompare` with `numeric` so `agent-10` follows `agent-9` instead of
 * sorting between `agent-1` and `agent-2`; resource names carry numbers often
 * enough that codepoint order reads as a bug. `sensitivity: "base"` keeps case
 * from splitting otherwise-adjacent names apart.
 */
const compare = (a: string, b: string): number =>
  b.localeCompare(a, undefined, { numeric: true, sensitivity: "base" });

export function byNamespaceThenName<T>(
  identify: (item: T) => ResourceName,
): (a: T, b: T) => number {
  return (a, b) => {
    const left = identify(a);
    const right = identify(b);
    return (
      compare(left.namespace, right.namespace) || compare(left.name, right.name)
    );
  };
}

/**
 * A sorted copy, leaving the argument alone.
 *
 * A copy because the array often comes straight from a cache that other readers
 * share, and `sort` mutates in place — sorting the cached array would reorder it
 * under whoever else is holding it.
 */
export function sortedByNamespaceThenName<T>(
  items: readonly T[],
  identify: (item: T) => ResourceName,
): T[] {
  return [...items].sort(byNamespaceThenName(identify));
}

/** For the responses that carry a `namespace/name` ref. */
export const sortedByRef = <T extends { ref: string }>(items: readonly T[]): T[] =>
  sortedByNamespaceThenName(items, (item) => parseResourceRef(item.ref));

/** For the responses that carry `namespace` and `name` as their own fields. */
export const sortedByFields = <T extends { namespace?: string; name: string }>(
  items: readonly T[],
): T[] =>
  sortedByNamespaceThenName(items, (item) => ({
    namespace: item.namespace ?? "",
    name: item.name,
  }));
