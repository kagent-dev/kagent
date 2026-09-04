/**
 * The seam an app extension bundle hooks into.
 *
 * The core UI never imports extension code; an extension bundle imports this module and
 * registers what it wants to change, once, before the first request is made
 * (from its own entry module, which the app loads ahead of rendering). Two
 * things can be changed without editing the application:
 *
 * 1. **What a call actually does** — `registerOperationOverride` replaces one
 *    operation's implementation, so a deployment can serve `models.list` from its
 *    own backend, a different API version, or a different protocol
 *    entirely.
 * 2. **What goes over the wire** — `registerApiTransform` wraps every call: add a
 *    header, send it somewhere else, inspect the request message.
 *
 * ## Why an override, where there used to be a path
 *
 * Under REST, re-pointing a call meant re-pointing a URL, and a string was
 * enough to say it. The application API is gRPC-Web now: the address of a call is
 * `<package>.<Service>/<Method>`, fixed by the generated descriptor, and there is
 * nothing string-shaped left to substitute. The equivalent freedom is to replace
 * the *implementation* — which is strictly more than the path override could
 * express, and is what a distribution needs in order to answer some operations from
 * a second backend.
 *
 * Both registries are additive and reversible: each register call returns an
 * unregister function. Transforms run in registration order on the way out and in
 * reverse order on the way back, so a pair of transforms nests rather than
 * interleaves.
 *
 * @example
 * // In an extension bundle's entry module:
 * registerOperationOverride("models.list", () => managedModels());
 * registerApiTransform({
 *   name: "tenant-header",
 *   request: (ctx) => ({
 *     ...ctx,
 *     headers: { ...ctx.headers, "X-Tenant": currentTenant() },
 *   }),
 * });
 */

import type { ApiOperation, OperationCallOptions, OperationId } from "./operations";

/** Anything a transform can be aimed at. */
export type ApiCallId = OperationId;

/** A call, after it has been built and before it is sent. */
export interface ApiRequestContext {
  /** Which logical call this is, so a transform can target one. */
  readonly endpoint: ApiCallId;
  /**
   * Always `POST`, which is what gRPC-Web puts on the wire.
   */
  readonly method: string;
  /**
   * The URL the call is going to.
   *
   * This is **informational**. A gRPC method is addressed from its own
   * descriptor and the transport's base URL, so a rewritten URL here changes
   * nothing about where the call lands; it is assembled so a transform can *decide*
   * on the strength of where a call is going. Moving the RPCs is
   * `registerApiBaseUrlResolver` — a whole-root override, which is the granularity
   * gRPC actually offers and the one a distribution proxying a cluster's API under its
   * own prefix needs.
   */
  url: string;
  headers: Record<string, string>;
  /**
   * The request message, for an RPC.
   *
   * Readable and replaceable, but it must stay the same message type — the
   * transport is about to encode it against the method's descriptor, and anything
   * else fails there rather than here.
   */
  message?: unknown;
}

/** A response, before it reaches the caller. */
export interface ApiResponseContext {
  readonly endpoint: ApiCallId;
  /** The status the gRPC code stands for. */
  readonly status: number;
  readonly url: string;
}

export interface ApiTransform {
  /** Identifies the transform in errors and when unregistering. */
  name: string;
  request?: (
    context: ApiRequestContext,
  ) => ApiRequestContext | Promise<ApiRequestContext>;
  response?: (body: unknown, context: ApiResponseContext) => unknown | Promise<unknown>;
}

/**
 * How an override is stored.
 *
 * `never` as the input type is what lets one map hold operations with unrelated
 * inputs: every concrete `ApiOperation<K>` is assignable to this, and the cast
 * back happens in one place, in `getOperationOverride`, where the id proves the
 * type.
 */
type StoredOperation = (
  input: never,
  options: OperationCallOptions,
) => Promise<unknown>;

const operationOverrides = new Map<OperationId, StoredOperation>();
const transforms: ApiTransform[] = [];

/**
 * Replaces what one operation does.
 *
 * The implementation is handed the same input the default would have received,
 * so an override can delegate: read the input, call its own backend, and return
 * this app's domain type. It is *not* wrapped in the transport's transforms —
 * a replacement implementation owns its own request entirely.
 *
 * @returns a function that removes the override again.
 */
export function registerOperationOverride<K extends OperationId>(
  operation: K,
  implementation: ApiOperation<K>,
): () => void {
  operationOverrides.set(operation, implementation);
  return () => {
    if (operationOverrides.get(operation) === implementation) {
      operationOverrides.delete(operation);
    }
  };
}

/**
 * Adds a request/response transform to every call the API client makes.
 *
 * @returns a function that removes the transform again.
 */
export function registerApiTransform(transform: ApiTransform): () => void {
  transforms.push(transform);
  return () => {
    const index = transforms.indexOf(transform);
    if (index !== -1) transforms.splice(index, 1);
  };
}

/** Drops every registration. Intended for tests. */
export function clearApiExtensions(): void {
  operationOverrides.clear();
  transforms.length = 0;
}

/** @internal — read by `invoke`. */
export function getOperationOverride<K extends OperationId>(
  operation: K,
): ApiOperation<K> | undefined {
  // The map is keyed by the id, and the id is what fixes the input and output
  // types, so this cast restores exactly what `registerOperationOverride` erased.
  return operationOverrides.get(operation) as ApiOperation<K> | undefined;
}

/** @internal — true when any transform is registered, so the transport can skip the work. */
export function hasApiTransforms(): boolean {
  return transforms.length > 0;
}

/** @internal — read by the transport and by the chat client. */
export async function applyRequestTransforms(
  context: ApiRequestContext,
): Promise<ApiRequestContext> {
  let current = context;
  for (const transform of transforms) {
    if (transform.request) current = await transform.request(current);
  }
  return current;
}

/** @internal — read by the transport. Runs in reverse so transforms nest. */
export async function applyResponseTransforms(
  body: unknown,
  context: ApiResponseContext,
): Promise<unknown> {
  let current = body;
  for (let i = transforms.length - 1; i >= 0; i -= 1) {
    const transform = transforms[i];
    if (transform.response) current = await transform.response(current, context);
  }
  return current;
}
