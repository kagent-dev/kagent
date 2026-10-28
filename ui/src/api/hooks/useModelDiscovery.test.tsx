import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "../ApiError";
import { apiClient } from "../client";
import { useModelDiscovery } from "./useModels";

function wrapper({ children }: { children: ReactNode }) {
  return <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false, revalidateOnFocus: false }}>{children}</SWRConfig>;
}

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); });

it("recovers from a transient polling failure and resumes polling", async () => {
  vi.useFakeTimers();
  vi.spyOn(Math, "random").mockReturnValue(0.5);
  const catalog = { endpoint: "https://gateway/v1", state: "current" as const, models: [{ id: "model-a" }] };
  const read = vi.spyOn(apiClient.models, "discovery")
    .mockResolvedValueOnce(catalog)
    .mockRejectedValueOnce(new ApiError("Unavailable", { kind: "network", status: 503, url: "discovery" }))
    .mockResolvedValue(catalog);
  const { result } = renderHook(() => useModelDiscovery("team", "gateway"), { wrapper });
  await act(async () => { await vi.advanceTimersByTimeAsync(0); });
  expect(result.current.data).toEqual(catalog);
  await act(async () => { await vi.advanceTimersByTimeAsync(10000); });
  expect(result.current.error).toBeDefined();
  await act(async () => { await vi.advanceTimersByTimeAsync(10000); });
  expect(result.current.error).toBeUndefined();
  expect(read).toHaveBeenCalledTimes(3);
  await act(async () => { await vi.advanceTimersByTimeAsync(10000); });
  expect(read).toHaveBeenCalledTimes(4);
});

it.each([403, 404])("does not retry a permanent %s error", async status => {
  vi.useFakeTimers();
  const read = vi.spyOn(apiClient.models, "discovery").mockRejectedValue(
    new ApiError("Unavailable source", { kind: "http", status, url: "discovery" }));
  const { result } = renderHook(() => useModelDiscovery("team", "gateway"), { wrapper });
  await act(async () => { await vi.advanceTimersByTimeAsync(120000); });
  expect(result.current.error?.status).toBe(status);
  expect(read).toHaveBeenCalledTimes(1);
});

it("bounds retries when the service stays unavailable", async () => {
  vi.useFakeTimers();
  vi.spyOn(Math, "random").mockReturnValue(0.5);
  const read = vi.spyOn(apiClient.models, "discovery").mockRejectedValue(
    new ApiError("Unavailable", { kind: "network", status: 503, url: "discovery" }));
  renderHook(() => useModelDiscovery("team", "gateway"), { wrapper });
  await act(async () => { await vi.advanceTimersByTimeAsync(1000000); });
  expect(read).toHaveBeenCalledTimes(6);
});
