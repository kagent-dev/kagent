/** An `ate.dev` WorkerPool custom resource. */
export interface SubstrateWorkerPoolEntry {
  namespace: string;
  name: string;
  replicas: number;
  ateomImage: string;
}

/** An ATE ActorTemplate, identified by atespace and name. */
export interface SubstrateActorTemplateEntry {
  atespace: string;
  name: string;
  phase?: string;
  goldenActorId?: string;
  goldenSnapshot?: string;
  sandboxClass?: string;
  workerSelector?: string;
}

/** Runtime actor state, from ate-api rather than from Kubernetes. */
export interface SubstrateActorEntry {
  actorId: string;
  atespace: string;
  status: string;
  actorTemplateAtespace?: string;
  actorTemplateName?: string;
  ateomPodNamespace?: string;
  ateomPodName?: string;
  ateomPodIp?: string;
  latestSnapshot?: string;
  workerPoolName?: string;
  inProgressSnapshot?: string;
  version?: number;
}

/** A worker reports capacity and allocation, but no actor reference. */
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
  /** Every actor in the requested atespace scope. */
  actorCount: number;
  workerCount: number;
  /** The numerators the inventory is actually read by: how much of it is working. */
  runningActorCount: number;
  /** Workers reporting a positive allocated actor count. */
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

/** One upstream page; an empty page may still have a continuation token. */
interface SubstratePage extends Timed {
  enabled: boolean;
  /** Upstream read failure. No rows or continuation token accompany it. */
  ateApiError?: string;
  nextPageToken?: string;
}

export interface SubstrateActorPage extends SubstratePage {
  actors: SubstrateActorEntry[];
}

export interface SubstrateWorkerPage extends SubstratePage {
  workers: SubstrateWorkerEntry[];
}
