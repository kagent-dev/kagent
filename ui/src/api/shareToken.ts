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

/**
 * The operations that are about one conversation.
 *
 * Enumerated rather than pattern-matched. Under REST this was a check on the URL
 * path, and that check had a trap in it — `/sessions/{id}` is a prefix of
 * `/sessions/{id}-and-more`, which is a different conversation — so the test had to
 * be about where the segment *ended*. An operation id has no prefixes, so the whole
 * class of mistake is gone.
 *
 * `chat.a2a` is deliberately not here. A visitor holding a read-only link must not
 * be able to post to the agent, and the token is not what authorises them to when
 * the link is read-write — their own identity is.
 */
const SESSION_OPERATIONS = new Set<ApiCallId>([
  "sessions.get",
  "sessions.tasks",
  "sessions.shares.list",
  "sessions.shares.create",
  "sessions.shares.delete",
]);

/**
 * Adds the token to calls about one conversation, and to nothing else.
 *
 * Scoped rather than sent on everything, because the token is a credential for
 * exactly one conversation and the visitor is signed in as themselves for the rest
 * of the app. A transform that attached it to every call would hand a session
 * token to operations that have no business seeing it — and, the part that
 * actually bites, a stale registration would keep doing so after the reader had
 * navigated away.
 *
 * The session is read from the request message rather than from the address, so a
 * call about a *different* conversation is left alone even though it is the same
 * operation.
 */
export function withShareToken(
  context: ApiRequestContext,
  sessionId: string,
  token: string,
): ApiRequestContext {
  if (!SESSION_OPERATIONS.has(context.endpoint)) return context;
  if (sessionIdOf(context.message) !== sessionId) return context;

  return { ...context, headers: { ...context.headers, [SHARE_HEADER]: token } };
}

/**
 * The conversation a request message names.
 *
 * Every session RPC carries it as `session_id`, which the generated TypeScript
 * spells `sessionId`. A message without one is not about a conversation, so it
 * gets no token.
 */
function sessionIdOf(message: unknown): string | undefined {
  if (!message || typeof message !== "object") return undefined;
  const value = (message as { sessionId?: unknown }).sessionId;
  return typeof value === "string" ? value : undefined;
}

/**
 * Spends a share token for as long as the component is mounted.
 *
 * Registered on mount and removed on cleanup, so the credential lives exactly as
 * long as the page that was opened with it — and never outlives it, which a
 * module-level token would.
 *
 * ## Why a layout effect, and why the call order matters
 *
 * A *passive* effect is too late. SWR triggers its first read from a layout effect,
 * and layout effects all run before any passive one — so the transcript request
 * went out before the token was registered, without the header, and the backend
 * answered as though nobody had a share. It looked like it worked: the fixture
 * served the conversation to an anonymous read, and only asking the mock to
 * *refuse* an unknown token showed the header was missing.
 *
 * Within one component, effects of the same kind run in the order their hooks were
 * called. So this must be called *above* whatever reads the conversation, and being
 * a layout effect is what puts it ahead of SWR's own.
 */
export function useShareToken(sessionId: string | undefined, token: string | undefined) {
  useLayoutEffect(() => {
    if (!sessionId || !token) return;

    return registerApiTransform({
      name: "shareToken",
      request: (context) => withShareToken(context, sessionId, token),
    });
  }, [sessionId, token]);
}

/*
 * The AgentInstance half of sharing.
 *
 * A share over an instance cannot go through the transform chain the session one
 * uses, and the reason is worth stating rather than working around twice: the A2A
 * calls are made against the generated `A2AService` client directly and carry no
 * operation id, so `transformInterceptor` passes them straight through. Giving them
 * one would mean inventing operation ids for a client that is deliberately not part
 * of the operation table.
 *
 * So the token is registered here and the chat client reads it. One registration
 * point and one header name for both kinds of share — the alternative, a second
 * mechanism beside this one, is exactly the drift this file already warns about.
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
