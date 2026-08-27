import { useLayoutEffect } from "react";
import { registerApiTransform } from "./extensionPoints";
import type { ApiCallId, ApiRequestContext } from "./extensionPoints";

/**
 * The header a share link is spent with.
 *
 * A share is a capability: the backend resolves the token to the conversation's
 * owner and answers as though the owner had asked, while keeping the caller's own
 * identity for the record. The gRPC server reads it from call metadata under this
 * name (`authenticationUnaryInterceptor` in `go/core/internal/grpcserver`), and
 * gRPC-Web carries metadata as HTTP headers, so setting the header is what puts it
 * in the metadata.
 */
const SHARE_HEADER = "X-Share-Token";

/*
 * AgentInstance sharing.
 *
 * A2A calls cannot go through the ordinary operation transform chain: they are made
 * against the generated `A2AService` client directly and carry no operation id, so
 * `transformInterceptor` passes them straight through. Giving them one would mean
 * inventing operation ids for a client deliberately outside the operation table.
 *
 * So the token is registered here and the chat client reads it. The ordinary
 * AgentInstance operations use the same registration and header below.
 */

/** Which conversation a registered instance share is for. */
let sharedInstance: { key: string; token: string } | undefined;

const instanceKey = (namespace: string, id: string) => `${namespace}/${id}`;

/**
 * The share token to send for this conversation, if one is registered.
 *
 * Scoped to the conversation rather than global, for the same reason the session
 * transform is: the token is a credential for exactly one conversation, and the
 * visitor is signed in as themselves for the rest of the app.
 */
export function agentInstanceShareToken(
  namespace: string,
  id: string,
): string | undefined {
  const key = instanceKey(namespace, id);
  return sharedInstance?.key === key ? sharedInstance.token : undefined;
}

/**
 * Spends a share token for one AgentInstance for as long as the page is mounted.
 *
 * A layout effect for the reason the session one is: SWR and the chat client both
 * start reading from a layout effect, and a passive effect would register the token
 * *after* the first request had already gone without it — which the backend answers
 * as an anonymous read, and which looks like success.
 */
export function useAgentInstanceShareToken(
  namespace: string | undefined,
  id: string | undefined,
  token: string | undefined,
) {
  useLayoutEffect(() => {
    if (!namespace || !id || !token) return;
    sharedInstance = { key: instanceKey(namespace, id), token };
    /*
     * And on the ordinary operations too, not only on the chat client.
     *
     * The A2A calls need the registration above because they bypass the operation
     * table; everything else about a conversation goes through it, and until this
     * existed none of it carried the token. A visitor could talk to a shared
     * conversation and could not read its record or give its worker back, which left
     * the shared page holding a live agent it had no way to suspend.
     */
    const unregister = registerApiTransform({
      name: "agentInstanceShareToken",
      request: (context) => withInstanceShareToken(context, namespace, id, token),
    });
    return () => {
      sharedInstance = undefined;
      unregister();
    };
  }, [namespace, id, token]);
}

/**
 * Operations a share over one conversation is authority for.
 *
 * Enumerated for the reason the session set is, and kept to the lifecycle a visitor
 * can legitimately reach. Deleting is not here: a share is permission to use somebody
 * else's conversation, not to destroy it, and the controller would refuse it anyway —
 * an entry here would only turn a clear refusal into a confusing one.
 *
 * Read-only shares are not filtered here either. The backend refuses any non-read RPC
 * for one before it reaches the service, which is where that rule belongs; a second
 * copy of it in the client would be a rule that could disagree with itself.
 */
const INSTANCE_OPERATIONS = new Set<ApiCallId>([
  "agentInstances.get",
  "agentInstances.suspend",
  "agentInstances.resume",
]);

/**
 * Adds the token to calls about one conversation, and to nothing else.
 *
 * The instance is read from the request message rather than from the address, so a
 * call about a different conversation is left alone even though it is the same
 * operation — the same scoping the session transform does, for the same reason.
 */
export function withInstanceShareToken(
  context: ApiRequestContext,
  namespace: string,
  id: string,
  token: string,
): ApiRequestContext {
  if (!INSTANCE_OPERATIONS.has(context.endpoint)) return context;
  const message = context.message as
    | { namespace?: unknown; agentInstanceId?: unknown }
    | undefined;
  if (message?.namespace !== namespace || message?.agentInstanceId !== id) {
    return context;
  }
  return { ...context, headers: { ...context.headers, [SHARE_HEADER]: token } };
}
