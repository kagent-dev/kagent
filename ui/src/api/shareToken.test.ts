import { describe, expect, it } from "vitest";
import { withShareToken } from "./shareToken";
import type { ApiCallId, ApiRequestContext } from "./extensionPoints";

/**
 * A share token is a credential for one conversation. What matters here is not
 * that it is sent, but that it is sent to precisely the calls it belongs on: too
 * narrow and a shared link shows an empty conversation, too wide and every
 * unrelated operation is handed a token for a session it was never asked about.
 *
 * The scoping test used to be on the URL path, and had to guard against
 * `/sessions/sess-1` matching `/sessions/sess-1-and-more`. It is now on the
 * operation id and the session named in the request message, so the identity is
 * compared as a whole value and the prefix problem does not exist. The cases below
 * are the same properties as before, asked of the new mechanism.
 */

const context = (
  call: ApiCallId,
  message?: unknown,
): ApiRequestContext => ({
  endpoint: call,
  method: "POST",
  url: "/api/kagent.api.v1alpha1.SessionService/GetSession",
  headers: { Accept: "application/json" },
  message,
});

const header = (call: ApiCallId, message?: unknown) =>
  withShareToken(context(call, message), "sess-1", "tok-abc").headers["X-Share-Token"];

describe("withShareToken", () => {
  it("sends the token when reading the conversation itself", () => {
    expect(header("sessions.get", { sessionId: "sess-1" })).toBe("tok-abc");
  });

  it("sends it when reading that conversation's turns", () => {
    expect(header("sessions.tasks", { sessionId: "sess-1" })).toBe("tok-abc");
  });

  it("sends it for that conversation's share links", () => {
    expect(header("sessions.shares.list", { sessionId: "sess-1" })).toBe("tok-abc");
    expect(
      header("sessions.shares.delete", { sessionId: "sess-1", token: "other" }),
    ).toBe("tok-abc");
  });

  it("leaves other conversations alone", () => {
    expect(header("sessions.get", { sessionId: "sess-2" })).toBeUndefined();
  });

  // The whole class of failure the old path matching had to defend against: an id
  // this one is a prefix of is a different conversation.
  it("is not fooled by an id this one is a prefix of", () => {
    expect(header("sessions.get", { sessionId: "sess-1-and-more" })).toBeUndefined();
  });

  it("leaves the rest of the app alone", () => {
    expect(header("agents.list", {})).toBeUndefined();
    expect(header("sessions.listForAgent", { namespace: "kagent", name: "k8s" }))
      .toBeUndefined();
  });

  /**
   * A visitor with a read-only link must not be able to post to the agent, and a
   * visitor with a read-write one is authorised by their own identity rather than
   * by this token. Either way chat is not something the token belongs on.
   */
  it("never attaches to the conversation's A2A endpoint", () => {
    expect(header("chat.a2a", { sessionId: "sess-1" })).toBeUndefined();
  });

  it("declines a session call that names no session", () => {
    expect(header("sessions.get")).toBeUndefined();
    expect(header("sessions.get", { sessionId: 7 })).toBeUndefined();
  });

  it("keeps the headers the request already had", () => {
    const result = withShareToken(
      context("sessions.get", { sessionId: "sess-1" }),
      "sess-1",
      "tok-abc",
    );

    expect(result.headers.Accept).toBe("application/json");
  });
});
