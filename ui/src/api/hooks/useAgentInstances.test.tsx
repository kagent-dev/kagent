import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it } from "vitest";
import type { AgentInstance } from "@/api";
import { ApiError } from "../ApiError";
import { apiClient } from "../client";
import { useAgentInstances } from "./useAgentInstances";

function conversation(id: string): AgentInstance {
  return {
    id,
    name: "",
    creator: "",
    agent: "team-a/assistant",
    state: "ready",
    operation: "unspecified",
    createdAt: "",
    updatedAt: "",
  };
}

function refusal(): ApiError {
  return new ApiError("permission denied: all_creators", {
    kind: "http",
    url: "kagent.api.v1.AgentInstanceService/ListAgentInstances",
    status: 403,
    code: "PermissionDenied",
  });
}

/** A fresh SWR cache per mount, so one test's read is never served to the next. */
function wrapper({ children }: { children: ReactNode }) {
  return <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0, shouldRetryOnError: false, onError: () => {} }}>{children}</SWRConfig>;
}

type ListOptions = { allCreators?: boolean };

describe("useAgentInstances", () => {
  const original = apiClient.agentInstances.list;
  /** The `allCreators` value of each read the hook made, in order. */
  let reads: boolean[] = [];

  function answerWith(read: (options: ListOptions) => Promise<AgentInstance[]>) {
    reads = [];
    apiClient.agentInstances.list = ((options: ListOptions = {}) => {
      reads.push(options.allCreators ?? false);
      return read(options);
    }) as typeof original;
  }

  afterEach(() => {
    apiClient.agentInstances.list = original;
  });

  it("returns the wide read when it is allowed", async () => {
    answerWith(async () => [conversation("a"), conversation("b")]);

    const { result } = renderHook(() => useAgentInstances(true), { wrapper });
    await waitFor(() => expect(result.current.data).toHaveLength(2));

    expect(result.current.widerReadRefused).toBeUndefined();
    expect(reads).toEqual([true]);
  });

  it("falls back to the caller's own conversations when the wide read is refused", async () => {
    answerWith(async (options) => {
      if (options.allCreators) throw refusal();
      return [conversation("mine")];
    });

    const { result } = renderHook(() => useAgentInstances(true), { wrapper });
    await waitFor(() => expect(result.current.data).toEqual([conversation("mine")]));

    expect(result.current.error).toBeUndefined();
    expect(result.current.widerReadRefused).toBe("permission denied: all_creators");
    expect(reads).toEqual([true, false]);
  });

  it("still reports a failure that is not a refusal", async () => {
    answerWith(async () => {
      throw new ApiError("backend down", { kind: "http", url: "x", status: 503, code: "Unavailable" });
    });

    const { result } = renderHook(() => useAgentInstances(true), { wrapper });
    await waitFor(() => expect(result.current.error?.message).toBe("backend down"));

    expect(result.current.data).toBeUndefined();
    expect(reads).toEqual([true]);
  });

  it("does not retry a refused read of the caller's own conversations", async () => {
    answerWith(async () => {
      throw refusal();
    });

    const { result } = renderHook(() => useAgentInstances(), { wrapper });
    await waitFor(() => expect(result.current.error?.status).toBe(403));

    expect(reads).toEqual([false]);
  });
});
