import {
  apiBaseUrl,
  registerApiBaseUrlResolver,
  registerApiTransform,
  registerOperationOverride,
} from "@/api";
import type { ApiCallId, OperationId } from "@/api";
import type { ExtensionApi } from "./extensionApi";
import type { AppExtensionConfig } from "../types";

/**
 * Installs every installed extension's API config, in order.
 *
 * Order is what makes the result predictable when two extensions both have an
 * opinion, and the three registries the data layer keeps differ on purpose:
 * operation and base-URL overrides are single-valued, so the last
 * registration wins — the later extension in the array. Transforms are a list
 * applied in registration order, so they *compose*: two extensions each adding a
 * header both get their header, and the later one sees the earlier one's work.
 */
export function installExtensionApis(
  extensions: readonly AppExtensionConfig[],
): () => void {
  const undo = extensions.map((extension) =>
    installExtensionApi(extension.api, extension.id),
  );

  // Unwound in reverse, so removing an install leaves the registries exactly as the
  // extensions before it had them.
  return () => {
    for (let i = undo.length - 1; i >= 0; i -= 1) undo[i]();
  };
}

/**
 * Installs one extension's declarative API config into the data layer's registries.
 *
 * This is the whole bridge between the two halves: the config says *what* differs
 * per call, the registries are *how* the client finds out. Every registration is
 * undone by the returned function, so a test can install an extension and clean up
 * after itself.
 *
 * Must run before the first request. The app does this as a module side effect —
 * see `installActiveExtensions.ts`.
 *
 * `id` only names the registered transforms. With several extensions installed
 * there are several of each, and a transform whose name says nothing about which
 * extension registered it is not much help in a stack trace.
 */
export function installExtensionApi(
  extension: ExtensionApi<ApiCallId> | undefined,
  id = "appExtension",
): () => void {
  if (!extension) return () => {};

  const undo: Array<() => void> = [];

  // Replacement implementations first, so an extension that answers an operation
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
    // finished request. The transform below keeps the informational URL consistent
    // for request hooks.
    undo.push(registerApiBaseUrlResolver(root));

    undo.push(
      registerApiTransform({
        name: `${id}:baseUrl`,
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
        name: `${id}:transforms`,
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
    undo.push(registerApiTransform({ name: `${id}:request`, request }));
  }

  return () => {
    for (const undoOne of undo) undoOne();
  };
}
