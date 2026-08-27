import type {
  ApiOperation,
  ApiRequestContext,
  ApiResponseContext,
  EndpointId,
  OperationId,
} from "@/api";

/**
 * The API-layer extension contract.
 *
 * An extension's backend rarely lives at the same URLs as this application's,
 * and rarely speaks the same payload shapes, so an extension needs to say where
 * a call goes and how to reshape it in both directions.
 *
 * The data layer already owns the runtime seam — registries of operation
 * overrides, endpoint overrides and transforms, plus the resolution logic that
 * reads them. This module deliberately does not restate any of that. What it adds
 * is the declarative half: the shape those changes take inside an extension's own
 * config object, keyed per call, so an extension declares its API differences
 * alongside its nav items and slots rather than in an imperative bundle entry
 * point. `installExtensionApis` folds every installed extension's shape into the
 * registries, in order.
 *
 * The transform hooks are typed against the data layer's own request and
 * response contexts, so there is exactly one description of a request in the
 * codebase.
 */

/**
 * Reshapes one endpoint's traffic. Both hooks are optional — an extension that only
 * needs a different URL supplies neither.
 *
 * Scoped to a single call, unlike the registry's global `ApiTransform`: the
 * installer is what turns a table of these into one registry entry that
 * dispatches on the call's id, so config authors never write that filter.
 */
export interface ExtensionEndpointTransform {
  /** Runs after the call has been built, before it is sent. */
  request?: (
    context: ApiRequestContext,
  ) => ApiRequestContext | Promise<ApiRequestContext>;
  /** Runs on the response payload before it reaches the caller. */
  response?: (
    body: unknown,
    context: ApiResponseContext,
  ) => unknown | Promise<unknown>;
}

/**
 * An extension's API overrides. `TCallId` is the data layer's own union of operation
 * and endpoint ids, so naming a call that does not exist is a compile error.
 */
export interface ExtensionApi<TCallId extends string = string> {
  /**
   * Replaces the host's API root for every call. Applied as a rewrite of the
   * resolved URL's prefix, since the base URL itself belongs to the data
   * layer's own configuration.
   *
   * A function is resolved per request, for an extension whose root can change
   * while the app is open — a tenant, region or cluster selector moves every
   * subsequent call to a different root. A fixed string cannot express that, and
   * the only way to honour it would be to reload the document. Returning
   * `undefined` from the function leaves the application's own base URL alone,
   * exactly as omitting the field does.
   */
  baseUrl?: string | (() => string | undefined);
  /**
   * Per-endpoint path overrides, relative to the API base URL.
   *
   * Only the calls still served over HTTP have a path to override — everything
   * else is an RPC whose address the generated descriptor fixes. Use `operations`
   * for those.
   */
  endpoints?: Partial<Record<EndpointId, string>>;
  /**
   * Replacement implementations, per operation.
   *
   * The gRPC equivalent of a path override, and strictly more: the implementation
   * receives the operation's own input and returns its own domain type, so it can
   * answer from a different service, a different protocol, or from nothing at all.
   * This is how a backend that owns some of these resources itself serves
   * them without the application knowing.
   */
  operations?: { [K in OperationId]?: ApiOperation<K> };
  /** Per-call request/response reshaping. */
  transforms?: Partial<Record<TCallId, ExtensionEndpointTransform>>;
  /**
   * Applied to *every* request, after `baseUrl` and the per-endpoint transforms.
   *
   * For what a backend requires of all its traffic rather than of one
   * endpoint — an authorization header, a tenant header, a correlation id. The
   * per-endpoint table cannot express that: it would mean an entry per endpoint,
   * and the endpoint added next week would silently go out without it.
   *
   * Runs last so it sees the final URL, which is what lets it decide based on
   * where the request is actually going.
   */
  request?: (
    context: ApiRequestContext,
  ) => ApiRequestContext | Promise<ApiRequestContext>;
}
