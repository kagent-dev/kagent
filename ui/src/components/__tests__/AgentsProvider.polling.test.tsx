import { describe, expect, it, jest, beforeEach, afterEach } from "@jest/globals";
import React from "react";
import { render, act, waitFor } from "@testing-library/react";
import { AgentsProvider, useAgents } from "../AgentsProvider";

// ── Mocks ───────────────────────────────────────────────────────────────────

const mockGetAgents = jest.fn();
const mockGetTools = jest.fn();
const mockGetModelConfigs = jest.fn();

jest.mock("@/app/actions/agents", () => ({
  getAgents: (...args: unknown[]) => mockGetAgents(...args),
  getAgent: jest.fn().mockResolvedValue({ data: null }),
  createAgent: jest.fn().mockResolvedValue({ data: {} }),
}));

jest.mock("@/app/actions/tools", () => ({
  getTools: (...args: unknown[]) => mockGetTools(...args),
}));

jest.mock("@/app/actions/modelConfigs", () => ({
  getModelConfigs: (...args: unknown[]) => mockGetModelConfigs(...args),
}));

// ── Helpers ─────────────────────────────────────────────────────────────────

function makeAgent(name: string, ready: boolean) {
  return {
    agent: { metadata: { name, namespace: "default" }, spec: {} },
    deploymentReady: ready,
  };
}

/** Renders an invisible consumer that exposes context values via a ref. */
function renderProvider() {
  const ref: { current: ReturnType<typeof useAgents> | null } = { current: null };
  function Consumer() {
    ref.current = useAgents();
    return null;
  }
  const utils = render(
    <AgentsProvider>
      <Consumer />
    </AgentsProvider>,
  );
  return { ref, ...utils };
}

// ── Tests ───────────────────────────────────────────────────────────────────

describe("AgentsProvider polling", () => {
  beforeEach(() => {
    jest.useFakeTimers();
    mockGetTools.mockResolvedValue([]);
    mockGetModelConfigs.mockResolvedValue({ data: [] });
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it("does not poll when all agents are ready", async () => {
    mockGetAgents.mockResolvedValue({
      data: [makeAgent("a", true), makeAgent("b", true)],
    });

    await act(async () => {
      renderProvider();
    });

    // Advance well past the 5 s poll interval
    await act(async () => {
      jest.advanceTimersByTime(15_000);
    });

    // Initial fetch only — no polling calls
    expect(mockGetAgents).toHaveBeenCalledTimes(1);
  });

  it("polls while at least one agent is not ready and stops when all become ready", async () => {
    // Initial fetch: one agent not ready
    mockGetAgents.mockResolvedValueOnce({
      data: [makeAgent("a", true), makeAgent("b", false)],
    });

    await act(async () => {
      renderProvider();
    });

    // First poll — still not ready
    mockGetAgents.mockResolvedValueOnce({
      data: [makeAgent("a", true), makeAgent("b", false)],
    });

    await act(async () => {
      jest.advanceTimersByTime(5_000);
    });

    // Second poll — now all ready
    mockGetAgents.mockResolvedValueOnce({
      data: [makeAgent("a", true), makeAgent("b", true)],
    });

    await act(async () => {
      jest.advanceTimersByTime(5_000);
    });

    // No more polls after becoming ready
    await act(async () => {
      jest.advanceTimersByTime(15_000);
    });

    // 1 initial + 2 polls = 3 total
    expect(mockGetAgents).toHaveBeenCalledTimes(3);
  });

  it("stops polling after 3 consecutive errors", async () => {
    // Initial: not ready
    mockGetAgents.mockResolvedValueOnce({
      data: [makeAgent("a", false)],
    });

    const { ref } = await act(async () => renderProvider());

    // 3 consecutive failures
    for (let i = 0; i < 3; i++) {
      mockGetAgents.mockResolvedValueOnce({ error: "network error", data: null });
      await act(async () => {
        jest.advanceTimersByTime(5_000);
      });
    }

    // Should have stopped — no more calls after advancing further
    const callsBefore = mockGetAgents.mock.calls.length;
    await act(async () => {
      jest.advanceTimersByTime(15_000);
    });

    expect(mockGetAgents.mock.calls.length).toBe(callsBefore);
    // Error should be surfaced
    await waitFor(() => {
      expect(ref.current?.error).toBeTruthy();
    });
  });
});
