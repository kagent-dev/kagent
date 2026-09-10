/**
 * Agent Substrate inventory.
 *
 * Mirrors `GetSubstrateStatusResponse` in `system.proto` field for field — this is one response the UI only ever reads, so drifting from the Go
 * shape would show up as blank columns rather than as a type error.
 */

/** `SystemService.GetSubstrateStatus` — controller and Kubernetes state, aggregated. */
export interface SubstrateStatusResponse {
  /** True when the controller is configured with an ate-api endpoint. */
  enabled: boolean;
  /**
   * Set when ate-api list calls failed.
   *
   * The response is still a success: `actors` and `workers` may be partial or
   * empty while the Kubernetes-derived halves are complete, so this is a warning
   * to surface beside the data rather than an error to throw.
   */
  ateApiError?: string;
  workerPools: SubstrateWorkerPoolEntry[];
  actorTemplates: SubstrateActorTemplateEntry[];
  actors: SubstrateActorEntry[];
  workers: SubstrateWorkerEntry[];
}

/** An `ate.dev` WorkerPool custom resource. */
export interface SubstrateWorkerPoolEntry {
  namespace: string;
  name: string;
  replicas: number;
  ateomImage: string;
}

/** An `ate.dev` ActorTemplate custom resource. */
export interface SubstrateActorTemplateEntry {
  namespace: string;
  name: string;
  phase?: string;
  goldenActorId?: string;
  goldenSnapshot?: string;
  sandboxClass?: string;
  workerSelector?: string;
  harnessName?: string;
}

/** Runtime actor state, from ate-api rather than from Kubernetes. */
export interface SubstrateActorEntry {
  actorId: string;
  atespace?: string;
  status: string;
  actorTemplateNamespace?: string;
  actorTemplateName?: string;
  ateomPodNamespace?: string;
  ateomPodName?: string;
  ateomPodIp?: string;
  latestSnapshot?: string;
  workerPoolName?: string;
  inProgressSnapshot?: string;
  version?: number;
}

/**
 * A worker, from ate-api.
 *
 * Which actor is on it is not here, and is not on the wire either. ate-api's `Worker`
 * carries capacity and allocation but no actor reference: the binding lives on the
 * *actor*, so filling these rows in would mean reading every actor in the cluster to
 * join them — the whole-inventory read the paged calls exist to remove. The summary's
 * `busyWorkerCount` is what that join is worth doing once for.
 */
export interface SubstrateWorkerEntry {
  workerNamespace: string;
  workerPool: string;
  workerPod: string;
  ip?: string;
  version?: number;
}

/**
 * The inventory as counts, plus the two lists that are inherently small.
 *
 * `SystemService.GetSubstrateSummary`. This is what the tiles are read from, and
 * it is the only place a *total* comes from: the actor and worker reads are pages,
 * and a page's length is not a total. Counting rows on screen and labelling the
 * result "Actors" is the specific failure the split introduced the risk of, so the
 * server counts instead.
 */
export interface SubstrateSummary extends Timed {
  /** True when the controller is configured with an ate-api endpoint. */
  enabled: boolean;
  /**
   * Set when the ate-api read failed on an otherwise successful call.
   *
   * The Kubernetes-derived halves below are complete; the counts may be short.
   * A warning to show beside the data, not an error to throw.
   */
  ateApiError?: string;
  workerPools: SubstrateWorkerPoolEntry[];
  actorTemplates: SubstrateActorTemplateEntry[];
  /** Every actor in scope, before any filter. */
  actorCount: number;
  workerCount: number;
  /** The numerators the inventory is actually read by: how much of it is working. */
  runningActorCount: number;
  /** A worker is busy when an actor is placed on it. */
  busyWorkerCount: number;
  /**
   * Every actor status present, with how many hold it, ordered by status.
   *
   * The whole distribution rather than the running count alone — knowing 12 of
   * 4,312 are running says nothing about the other 4,300.
   */
  actorStatusCounts: SubstrateStatusCount[];
}

export interface SubstrateStatusCount {
  status: string;
  count: number;
}

/**
 * When an answer was computed, which is not necessarily when it was received.
 *
 * The summary walks every ate-api page to count, which on a cluster holding 410,110
 * actors is seconds rather than milliseconds. Stamping the answer with when it was
 * computed lets the page show its age, so a reader can tell a cluster that is not
 * changing from a read that is not finishing.
 */
export interface Timed {
  /** RFC3339, or `undefined` when the controller did not say. */
  computedAt?: string;
}

import type {
  SubstrateActorSortField,
  SubstrateSortOrder,
  SubstrateWorkerSortField,
} from "../operations";

/** What every paged substrate read has in common. */
interface SubstratePage extends Timed {
  /** True when the controller is configured with an ate-api endpoint. */
  enabled: boolean;
  /**
   * Set when the ate-api read failed on an otherwise successful call.
   *
   * There are no rows with it. Every read walks all of ate-api's pages so that the
   * order and the filter mean the whole scope, and a walk that fails part-way has an
   * inventory it can neither order nor count — so the page comes back empty, with
   * `totalSize` zero and no `nextPageToken`.
   *
   * A warning to show beside the table rather than an error to throw: the call
   * succeeded, and the other reads on the page may well have too.
   */
  ateApiError?: string;
  /**
   * A token for the next page, or `undefined` on the last one.
   *
   * Absent rather than empty, so "there is more" is a question about presence and
   * a caller cannot accidentally send `""` and re-read page one.
   */
  nextPageToken?: string;
  /**
   * How many rows match the filter across every page.
   *
   * What makes "20 of 4,312" sayable. Without it a page can only report its own
   * length, which reads as the whole result.
   */
  totalSize: number;
}

/** One page of actors, ordered and narrowed across the whole inventory. */
export interface SubstrateActorPage extends SubstratePage {
  actors: SubstrateActorEntry[];
  /**
   * The order the server actually applied.
   *
   * Reported rather than assumed, so the table can say how its rows are sorted instead
   * of showing the control's own state — which would still read as "sorted by status"
   * if the request had been ignored.
   */
  appliedSortField: SubstrateActorSortField;
  appliedSortOrder: SubstrateSortOrder;
}

export interface SubstrateWorkerPage extends SubstratePage {
  workers: SubstrateWorkerEntry[];
  appliedSortField: SubstrateWorkerSortField;
  appliedSortOrder: SubstrateSortOrder;
}
