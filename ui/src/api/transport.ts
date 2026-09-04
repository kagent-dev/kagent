/**
 * How the app reaches the controller's gRPC services from a browser.
 *
 * The controller's application API is gRPC. A browser cannot speak gRPC, so the
 * controller wraps its gRPC server as gRPC-Web and routes those requests to it
 * ahead of the REST router, stripping a leading `/api` on the way
 * (`withGrpcWeb`/`trimAPIPrefix` in `go/core/internal/httpserver/server.go`).
 * From here, that means the address of a call is
 * `<apiBaseUrl>/<package>.<Service>/<Method>`, and `apiBaseUrl` is the same base
 * every other request in the app is built on — the chart's nginx proxies `/api`
 * to the controller, so it is same-origin.
 *
 * Being same-origin is also why there is no token to attach by default: the
 * deployment authenticates with an oauth2-proxy cookie, which the browser sends
 * on its own. `registerAuthTokenSource` exists for the deployments that instead
 * hold a bearer token in the page — gRPC-Web carries it as call metadata rather
 * than as a header the caller sets, which is why it cannot reuse anything the
 * chat client does.
 *
 * ## Why the interceptors wrap the transport instead of living inside it
 *
 * `createGrpcWebTransport` takes an `interceptors` option, and using it is the
 * obvious thing. It is wrong here, because `setApiTransport` substitutes the
 * transport — that is how the fixtures are served, with no service worker in the
 * path at all — and a substituted transport built elsewhere would carry none of
 * this file's interceptors. Mock mode would then exercise a different code path
 * from live, which is this codebase's most expensive recurring failure: a fixture,
 * a type and a test agreeing with each other while all three disagree with the
 * real thing.
 *
 * So `apiTransport()` returns a wrapper that applies the chain over *whichever*
 * inner transport is in force, and `setApiTransport` replaces only the inner one.
 * The fixtures therefore need no transform handling of their own and cannot drift
 * from this.
 *
 * The concrete thing that protects: AgentInstance sharing sets `X-Share-Token`
 * through a request transform. If that header stopped reaching the transport, a
 * shared conversation would be refused even though its ordinary API calls worked.
 */

import {
  createClient,
  createContextKey,
  createContextValues,
} from "@connectrpc/connect";
import type {
  Client,
  ContextValues,
  Interceptor,
  StreamRequest,
  StreamResponse,
  Transport,
  UnaryRequest,
  UnaryResponse,
} from "@connectrpc/connect";
import { createGrpcWebTransport } from "@connectrpc/connect-web";
import { create } from "@bufbuild/protobuf";
import type {
  DescMessage,
  DescMethodStreaming,
  DescMethodUnary,
  DescService,
  MessageInitShape,
} from "@bufbuild/protobuf";
import { REQUEST_TIMEOUT_MS, apiBaseUrl } from "./config";
import {
  applyRequestTransforms,
  applyResponseTransforms,
  hasApiTransforms,
} from "./extensionPoints";
import type { OperationId } from "./operations";

/**
 * Which operation a call belongs to, carried alongside it.
 *
 * A transform is registered against an operation id, and the interceptor that
 * runs transforms only sees a service and a method — which is not the same thing.
 * One operation can use multiple RPCs (`models.providers` merges two), so the id
 * has to travel with the call rather
 * than be reconstructed from it.
 */
export const operationContext = createContextKey<OperationId | undefined>(undefined, {
  description: "kagent operation id",
});

/** A pluggable bearer token, for deployments that authenticate with one. */
export type AuthTokenSource = () => string | undefined;

let tokenSource: AuthTokenSource | undefined;

/**
 * Supplies the bearer token every call carries.
 *
 * @returns a function that removes it again.
 */
export function registerAuthTokenSource(source: AuthTokenSource): () => void {
  tokenSource = source;
  return () => {
    if (tokenSource === source) tokenSource = undefined;
  };
}

let baseUrlResolver: (() => string | undefined) | undefined;

/**
 * Points the gRPC services at a different root.
 *
 * This — not a rewritten URL — is the gRPC re-pointing seam. A gRPC method is
 * addressed by its own descriptor, so there is no per-operation path to
 * substitute; what a deployment can change is the root the whole service surface
 * hangs off, which is the granularity a distribution actually needs when it proxies
 * a cluster's API under its own prefix.
 *
 * Resolved per transport build rather than captured, so a root that changes while
 * the app is open (a tenant or region selector) is honoured on the next call
 * instead of needing a reload. Installing or removing one drops the cached
 * transport, because a transport is built around its base URL.
 *
 * @returns a function that removes it again.
 */
export function registerApiBaseUrlResolver(
  resolver: () => string | undefined,
): () => void {
  baseUrlResolver = resolver;
  resetApiTransport();
  return () => {
    if (baseUrlResolver !== resolver) return;
    baseUrlResolver = undefined;
    resetApiTransport();
  };
}

/** The root the gRPC services are addressed from, with any trailing slash removed. */
export function grpcBaseUrl(): string {
  const configured = baseUrlResolver?.() ?? apiBaseUrl;
  return configured.endsWith("/") ? configured.slice(0, -1) : configured;
}

/** The address one call goes to, for an error message or a transform to read. */
export function grpcCallUrl(service: DescService, methodName: string): string {
  return `${grpcBaseUrl()}/${service.typeName}/${methodName}`;
}

/**
 * Whether there is a root to address the services from at all.
 *
 * True in the reference deployment always: `API_BASE_URL` defaults to `/api`, so
 * there is no state in which nothing is configured. It exists for the deployment
 * or extension whose base URL resolver can answer "nowhere" — a cluster selector
 * with no cluster picked — and it is the guard those callers check before building
 * a client, rather than each of them re-deriving the answer.
 *
 * It is deliberately *not* a reason to show fixtures. A page that quietly renders
 * mock data because the backend is unset looks healthy while showing data that was
 * never real.
 */
export function hasLiveBackend(): boolean {
  return grpcBaseUrl() !== "";
}

/**
 * Puts the bearer token on every call, when a deployment supplied one.
 *
 * Skipped entirely when there is none, so the ordinary cookie-authenticated
 * deployment does not send an empty `Authorization` header — which some proxies
 * treat as an attempt to authenticate and reject outright.
 */
const authInterceptor: Interceptor = (next) => (request) => {
  const token = tokenSource?.();
  if (token) request.header.set("Authorization", `Bearer ${token}`);
  return next(request);
};

/**
 * Runs the registered request and response transforms around each call.
 *
 * Headers are copied out and back rather than handed over live, so a transform
 * cannot half-apply: it returns a whole context or it does not run.
 *
 * A call with no operation id in its context is one this app did not make through
 * `invoke` — nothing registers transforms against it, so it is passed straight
 * through rather than being given a made-up id.
 */
const transformInterceptor: Interceptor = (next) => async (request) => {
  const operation = request.contextValues.get(operationContext);
  if (!operation || !hasApiTransforms()) return next(request);

  const context = await applyRequestTransforms({
    endpoint: operation,
    method: "POST",
    url: request.url,
    headers: headerRecord(request.header),
    message: request.stream ? undefined : request.message,
  });

  writeHeaders(request.header, context.headers);

  const response = await next(
    request.stream || context.message === undefined
      ? request
      : { ...request, message: context.message as typeof request.message },
  );
  if (response.stream) return response;

  const body = await applyResponseTransforms(response.message, {
    endpoint: operation,
    // gRPC has no status; a call that got this far succeeded, and the codes for
    // the ones that did not are on the `ApiError` the caller sees instead.
    status: 200,
    url: request.url,
  });

  // The message must still satisfy the method's output descriptor. A transform
  // that returns something else fails when the caller reads it, which is why
  // response transforms are for reshaping a payload rather than replacing it.
  return { ...response, message: body as typeof response.message };
};

/**
 * Every interceptor a call goes through, outermost first.
 *
 * Applied by `withApiInterceptors` with the same rule connect itself uses — the
 * *last* element is applied first and so ends up innermost — which makes this
 * array read the way the request travels: the token is attached, and then a
 * deployment's own transforms see it already there and can replace it.
 */
const API_INTERCEPTORS: Interceptor[] = [authInterceptor, transformInterceptor];

/** What an interceptor wraps: one call, in and out. Connect's `AnyFn` is internal. */
type CallFn = (
  request: UnaryRequest | StreamRequest,
) => Promise<UnaryResponse | StreamResponse>;

function headerRecord(headers: Headers): Record<string, string> {
  const record: Record<string, string> = {};
  headers.forEach((value, key) => {
    record[key] = value;
  });
  return record;
}

function writeHeaders(headers: Headers, record: Record<string, string>): void {
  for (const [key, value] of Object.entries(record)) headers.set(key, value);
}

/**
 * The same transport, with this app's interceptors around every unary call.
 *
 * The request is assembled here rather than by the inner transport because that is
 * what an interceptor operates on. `create` normalises the caller's init object
 * into a whole message first, so a transform inspecting `message` sees the fields
 * the wire will carry rather than whatever shorthand the call site used.
 *
 * `url` is assembled for a transform to *read*. Rewriting it does not move an RPC:
 * the inner transport addresses the method from its own base URL and the method's
 * descriptor. Moving the RPCs is `registerApiBaseUrlResolver`, which is documented
 * above as the seam for exactly that.
 *
 * Server-streaming calls go through the same chain, which they must: the chat
 * reply is `A2AService/SendStreamingMessage` over gRPC-Web, and a stream that
 * bypassed this would miss the bearer token and the share token while looking
 * like it worked — a shared conversation would silently be read unauthenticated.
 * This file used to say streams passed through untouched, which was true only
 * while nothing streamed.
 *
 * What a transform can do to a stream is narrower than for a unary call, and
 * deliberately so: headers are its to change, the messages are not. There is no
 * whole request message to inspect before it goes — there is a sequence of them,
 * arriving over time — so `message` is left undefined for a stream rather than
 * handing a transform something that is not the request.
 */
function withApiInterceptors(inner: Transport): Transport {
  return {
    async unary<I extends DescMessage, O extends DescMessage>(
      method: DescMethodUnary<I, O>,
      signal: AbortSignal | undefined,
      timeoutMs: number | undefined,
      header: HeadersInit | undefined,
      input: MessageInitShape<I>,
      contextValues?: ContextValues,
    ): Promise<UnaryResponse<I, O>> {
      const invokeInner: CallFn = async (request) => {
        if (request.stream) throw new Error("A unary call cannot become a stream.");
        return inner.unary(
          request.method as DescMethodUnary<I, O>,
          request.signal,
          timeoutMs,
          request.header,
          // Already a whole message, which is a valid init shape for the same
          // descriptor — the generic is just erased by the time we get here.
          request.message as MessageInitShape<I>,
          request.contextValues,
        );
      };

      // Right-to-left, so the last interceptor is applied first and ends up
      // innermost — the order connect documents for its own `interceptors` option.
      const call = API_INTERCEPTORS.reduceRight<CallFn>(
        (next, interceptor) => interceptor(next as never) as CallFn,
        invokeInner,
      );

      const request: UnaryRequest<I, O> = {
        stream: false,
        service: method.parent,
        method,
        requestMethod: "POST",
        url: grpcCallUrl(method.parent, method.name),
        header: new Headers(header),
        contextValues: contextValues ?? createContextValues(),
        // A call with no caller signal still needs one: `UnaryRequest.signal` is
        // not optional, and an interceptor may read it.
        signal: signal ?? new AbortController().signal,
        message: create(method.input, input),
      };

      return (await call(request)) as UnaryResponse<I, O>;
    },

    async stream<I extends DescMessage, O extends DescMessage>(
      method: DescMethodStreaming<I, O>,
      signal: AbortSignal | undefined,
      timeoutMs: number | undefined,
      header: HeadersInit | undefined,
      input: AsyncIterable<MessageInitShape<I>>,
      contextValues?: ContextValues,
    ): Promise<StreamResponse<I, O>> {
      const invokeInner: CallFn = async (request) => {
        if (!request.stream) throw new Error("A stream cannot become a unary call.");
        return inner.stream(
          request.method as DescMethodStreaming<I, O>,
          request.signal,
          timeoutMs,
          request.header,
          request.message as AsyncIterable<MessageInitShape<I>>,
          request.contextValues,
        );
      };

      const call = API_INTERCEPTORS.reduceRight<CallFn>(
        (next, interceptor) => interceptor(next as never) as CallFn,
        invokeInner,
      );

      const request: StreamRequest<I, O> = {
        stream: true,
        service: method.parent,
        method,
        requestMethod: "POST",
        url: grpcCallUrl(method.parent, method.name),
        header: new Headers(header),
        contextValues: contextValues ?? createContextValues(),
        signal: signal ?? new AbortController().signal,
        // The caller's init objects, normalised the way the unary path normalises
        // its one message — so a descriptor that expects whole messages gets them
        // whatever shorthand the call site used.
        message: (async function* () {
          for await (const message of input) yield create(method.input, message);
        })(),
      };

      return (await call(request)) as StreamResponse<I, O>;
    },
  };
}

/**
 * The live transport, built once per root.
 *
 * Keyed by the resolved root rather than simply cached, so installing a base-URL
 * resolver cannot leave a transport pointed at the previous one — a bug whose
 * symptom is calls silently continuing to reach the old backend.
 *
 * No `interceptors` option here: the chain is applied above, by
 * `withApiInterceptors`, so that a substituted transport gets it too. Passing them
 * here as well would run every one of them twice.
 */
let cached: { baseUrl: string; transport: Transport } | undefined;
let inner: Transport | undefined;
const clients = new Map<string, unknown>();

function liveTransport(): Transport {
  const baseUrl = grpcBaseUrl();
  if (cached?.baseUrl === baseUrl) return cached.transport;

  const transport = createGrpcWebTransport({
    baseUrl,
    // A per-call deadline, set on the transport rather than in an interceptor: a
    // connect request carries no `init` to attach one to, so the transport's own
    // option is the only place it can go.
    defaultTimeoutMs: REQUEST_TIMEOUT_MS,
  });
  cached = { baseUrl, transport };
  return transport;
}

/** The transport every client is built on: the app's interceptors over the inner one. */
export function apiTransport(): Transport {
  return withApiInterceptors(inner ?? liveTransport());
}

/** Drops the cached transport and clients. Test seam, and used when the root changes. */
export function resetApiTransport(): void {
  cached = undefined;
  clients.clear();
}

/**
 * Serves every subsequent call from `transport` instead of from the network.
 *
 * This is the seam the fixtures and the unit suite run on: `createRouterTransport`
 * from `@connectrpc/connect` serves real service implementations in-process, so a
 * caller can assert which RPC was invoked with which message without a network
 * layer or a fixture HTTP server.
 *
 * What is substituted is the *inner* transport, so the app's own interceptors
 * still run: a substituted transport sees the share token, the bearer token and
 * every registered transform exactly as the live one does. Pass `undefined` to go
 * back to the network.
 */
export function setApiTransport(transport: Transport | undefined): void {
  resetApiTransport();
  inner = transport;
}

/**
 * A client for one service, memoised.
 *
 * Memoised because every operation asks for one and building a client walks the
 * service descriptor. Cleared alongside the transport, so a client can never
 * outlive the transport it was built on.
 */
export function serviceClient<T extends DescService>(service: T): Client<T> {
  const key = `${service.typeName}@${inner ? "substituted" : grpcBaseUrl()}`;

  const existing = clients.get(key);
  if (existing) return existing as Client<T>;

  const client = createClient(service, apiTransport());
  clients.set(key, client);
  return client;
}
