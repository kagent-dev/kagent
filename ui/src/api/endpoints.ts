/**
 * The paths still served over HTTP, behind a stable id.
 *
 * Almost nothing is left here. The controller moved its application API to gRPC
 * and serves it as gRPC-Web, so listing agents, writing a model config and
 * everything else is now an *operation* rather than a path — see `operations.ts`.
 * The A2A endpoint remains HTTP because that is the protocol agents expose.
 *
 * The table survives for that one endpoint rather than being inlined into the
 * chat client, because a deployment that routes the rest of the API somewhere
 * else has to be able to route chat with it. A deployment whose lists came from
 * one place and whose chat came from another would be a difficult thing to notice
 * and a worse thing to debug.
 */

export type EndpointParams = Record<string, string | undefined>;

/** Builds the path (relative to the API base URL) an endpoint resolves to. */
export type EndpointResolver = (params: EndpointParams) => string;

const defaultEndpoints = {
  /**
   * One agent's A2A endpoint — where a conversation is actually held.
   *
   * A `sandbox` param selects the Substrate-backed route.
   */
  "chat.a2a": (p: EndpointParams) =>
    `/${p.sandbox ? "a2a-sandboxes" : "a2a"}/${enc(p.namespace)}/${enc(p.name)}`,
} satisfies Record<string, EndpointResolver>;

export type EndpointId = keyof typeof defaultEndpoints;

/** Every endpoint id, for callers that need to enumerate them. */
export const endpointIds = Object.keys(defaultEndpoints) as EndpointId[];

const endpointOverrides = new Map<EndpointId, EndpointResolver>();

/**
 * Points an endpoint id at a different path.
 *
 * @returns a function that removes the override again.
 */
export function registerEndpointOverride(
  endpoint: EndpointId,
  resolver: EndpointResolver,
): () => void {
  endpointOverrides.set(endpoint, resolver);
  return () => {
    if (endpointOverrides.get(endpoint) === resolver) {
      endpointOverrides.delete(endpoint);
    }
  };
}

/** @internal — dropped by `clearApiExtensions`. */
export function clearEndpointOverrides(): void {
  endpointOverrides.clear();
}

/** The path an endpoint resolves to, honouring any registered override. */
export function resolveEndpoint(
  endpoint: EndpointId,
  params: EndpointParams = {},
): string {
  const resolver = endpointOverrides.get(endpoint) ?? defaultEndpoints[endpoint];
  return resolver(params);
}

/**
 * Escapes a path segment.
 *
 * A `:name` placeholder passes through untouched so a route pattern can be built
 * from this same table instead of restating the path.
 */
function enc(value: string | undefined): string {
  if (value?.startsWith(":")) return value;
  return encodeURIComponent(value ?? "");
}
