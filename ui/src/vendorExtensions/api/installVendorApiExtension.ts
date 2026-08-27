import {
  apiBaseUrl,
  registerApiBaseUrlResolver,
  registerApiTransform,
  registerEndpointOverride,
  registerOperationOverride,
} from "@/api";
import type { ApiCallId, EndpointId, OperationId } from "@/api";
import type { VendorApiExtension } from "./apiExtension";

/**
 * Installs a vendor's declarative API config into the data layer's registries.
 *
 * This is the whole bridge between the two halves: the config says *what* differs
 * per call, the registries are *how* the client finds out. Every registration is
 * undone by the returned function, so a test can install an extension and clean up
 * after itself.
 *
 * Must run before the first request. The app does this as a module side effect —
 * see `installActiveExtension.ts`.
 */
export function installVendorApiExtension(
  extension: VendorApiExtension<ApiCallId> | undefined,
): () => void {
  if (!extension) return () => {};

  const undo: Array<() => void> = [];

  // Replacement implementations first, so a vendor that answers an operation
  // itself is not also given the transforms meant for the application's own call.
  for (const [operation, implementation] of Object.entries(
    extension.operations ?? {},
  )) {
    if (typeof implementation !== "function") continue;
    undo.push(
      registerOperationOverride(
        operation as OperationId,
        implementation as Parameters<typeof registerOperationOverride>[1],
      ),
    );
  }

  for (const [endpoint, path] of Object.entries(extension.endpoints ?? {})) {
    if (typeof path !== "string") continue;
    undo.push(registerEndpointOverride(endpoint as EndpointId, () => path));
  }

  const { baseUrl } = extension;
  if (baseUrl !== undefined) {
    // Resolved per request rather than captured here. A root that can change while
    // the app is open — anything behind a tenant or region selector — would
    // otherwise be frozen at whatever it was when this ran, and the only way to
    // honour a change would be to reload the document.
    const resolveBase = typeof baseUrl === "function" ? baseUrl : () => baseUrl;
    const root = () => resolveBase()?.replace(/\/+$/, "") || undefined;

    // The gRPC transport is built around its base URL rather than given one per
    // call, so re-pointing it means telling the transport rather than rewriting a
    // finished request. Both halves are registered because both protocols are in
    // use: this one moves the RPCs, the transform below moves the HTTP endpoints.
    undo.push(registerApiBaseUrlResolver(root));

    undo.push(
      registerApiTransform({
        name: "vendorExtension:baseUrl",
        request: (context) => {
          if (!context.url.startsWith(apiBaseUrl)) return context;

          // Undefined means "leave the application's own base URL alone", which is
          // what an extension says when there is no alternative root to point at.
          const target = root();
          if (target === undefined) return context;

          return { ...context, url: `${target}${context.url.slice(apiBaseUrl.length)}` };
        },
      }),
    );
  }

  const transforms = extension.transforms;
  if (transforms && Object.keys(transforms).length > 0) {
    // One registry entry that dispatches on the call's id, so per-call config
    // never has to hand-write the "is this mine?" check.
    undo.push(
      registerApiTransform({
        name: "vendorExtension:transforms",
        request: async (context) => {
          const transform = transforms[context.endpoint];
          return transform?.request ? transform.request(context) : context;
        },
        response: async (body, context) => {
          const transform = transforms[context.endpoint];
          return transform?.response ? transform.response(body, context) : body;
        },
      }),
    );
  }

  // Registered last, so it runs after the base URL has been rewritten and after
  // any per-call transform — a hook deciding on the strength of the final address
  // has to see the final address.
  const { request } = extension;
  if (request !== undefined) {
    undo.push(registerApiTransform({ name: "vendorExtension:request", request }));
  }

  return () => {
    for (const undoOne of undo) undoOne();
  };
}
