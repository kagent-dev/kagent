import { describe, expect, it } from "vitest";
import { withInstanceShareToken } from "./shareToken";
import type { ApiCallId, ApiRequestContext } from "./extensionPoints";

const context = (endpoint: ApiCallId, message?: unknown): ApiRequestContext => ({
  endpoint,
  method: "POST",
  url: "/api/kagent.api.v1alpha1.AgentInstanceService/GetAgentInstance",
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
  it("attaches the token to reads and allowed lifecycle calls for its instance", () => {
    const message = { agentInstanceId: "instance-1" };
    expect(header("agentInstances.get", message)).toBe("tok-abc");
    expect(header("agentInstances.suspend", message)).toBe("tok-abc");
    expect(header("agentInstances.resume", message)).toBe("tok-abc");
  });

  it("does not attach the token to another instance", () => {
    expect(
      header("agentInstances.get", {
        agentInstanceId: "instance-2",
      }),
    ).toBeUndefined();
  });

  it("does not attach the token to destructive or unrelated operations", () => {
    const message = { agentInstanceId: "instance-1" };
    expect(header("agentInstances.delete", message)).toBeUndefined();
    expect(header("models.list", {})).toBeUndefined();
  });

  it("preserves existing headers", () => {
    const result = withInstanceShareToken(
      context("agentInstances.get", {
        agentInstanceId: "instance-1",
      }),

      "instance-1",
      "tok-abc",
    );
    expect(result.headers.Accept).toBe("application/grpc-web+proto");
  });
});
