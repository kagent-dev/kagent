import { describe, expect, it } from "vitest";
import { withInstanceShareToken } from "./shareToken";
import type { ApiCallId, ApiRequestContext } from "./extensionPoints";

const context = (endpoint: ApiCallId, message?: unknown): ApiRequestContext => ({
  endpoint,
  method: "POST",
  url: "/api/kagent.api.v1.SessionService/GetSession",
  headers: { Accept: "application/grpc-web+proto" },
  message,
});

const header = (endpoint: ApiCallId, message?: unknown) =>
  withInstanceShareToken(
    context(endpoint, message),

    "instance-1",
    "tok-abc",
  ).headers["X-Share-Token"];

describe("withInstanceShareToken", () => {
  it("attaches the token to reads of its instance", () => {
    const message = { sessionId: "instance-1" };
    expect(header("agentInstances.get", message)).toBe("tok-abc");
  });

  it("does not attach the token to another instance", () => {
    expect(
      header("agentInstances.get", {
        sessionId: "instance-2",
      }),
    ).toBeUndefined();
  });

  it("does not attach the token to mutations or unrelated operations", () => {
    const message = { sessionId: "instance-1" };
    expect(header("agentInstances.suspend", message)).toBeUndefined();
    expect(header("agentInstances.resume", message)).toBeUndefined();
    expect(header("agentInstances.delete", message)).toBeUndefined();
    expect(header("models.list", {})).toBeUndefined();
  });

  it("preserves existing headers", () => {
    const result = withInstanceShareToken(
      context("agentInstances.get", {
        sessionId: "instance-1",
      }),

      "instance-1",
      "tok-abc",
    );
    expect(result.headers.Accept).toBe("application/grpc-web+proto");
  });
});
