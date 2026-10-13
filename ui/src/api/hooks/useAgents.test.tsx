import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiClient } from "../client";
import type { Agent } from "../domain/agents";
import { SETTLING_AGENT_POLL_MS, useAgentsAcrossNamespaces } from "./useAgents";

function agent(desiredRevision: string): Agent {
  return {
    name: "a",
    namespace: "team",
    ref: "team/a",
    resource: {
      metadata: { name: "a", namespace: "team", generation: 1 },
      spec: { templateRef: { name: "shared" }, harnessRef: { name: "runner" } },
      status: { observedGeneration: 1, latestSuccessfulRevision: "good", desiredRevision },
    },
  };
}

function wrapper({ children }: { children: ReactNode }) {
  return <SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0 }}>{children}</SWRConfig>;
}

/** Lets SWR's fetch resolve and its state land without advancing the poll clock. */
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

describe("useAgentsAcrossNamespaces", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("re-reads an updating Agent until the controller reports it ready, then stops", async () => {
    const list = vi
      .spyOn(apiClient.agentBuildingBlocks, "agents")
      .mockResolvedValueOnce([agent("new")])
      .mockResolvedValue([agent("good")]);

    const { result } = renderHook(() => useAgentsAcrossNamespaces(["team"]), { wrapper });
    await settle();
    expect(list).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(SETTLING_AGENT_POLL_MS);
    });
    expect(list).toHaveBeenCalledTimes(2);
    expect(result.current.data?.agents[0].resource.status?.desiredRevision).toBe("good");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(SETTLING_AGENT_POLL_MS * 3);
    });
    expect(list).toHaveBeenCalledTimes(2);
  });

  it("makes no periodic reads while every Agent has settled", async () => {
    const list = vi.spyOn(apiClient.agentBuildingBlocks, "agents").mockResolvedValue([agent("good")]);

    renderHook(() => useAgentsAcrossNamespaces(["team"]), { wrapper });
    await settle();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(SETTLING_AGENT_POLL_MS * 3);
    });
    expect(list).toHaveBeenCalledTimes(1);
  });
});
