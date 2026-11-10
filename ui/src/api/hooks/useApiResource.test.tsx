import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { expect, it, vi } from "vitest";
import { ApiError } from "../ApiError";
import { useApiResource } from "./useApiResource";

function wrapper({ children }: { children: ReactNode }) {
  return <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false, dedupingInterval: 0 }}>{children}</SWRConfig>;
}

it("retains loaded data and exposes a failed refresh until a repaired retry succeeds", async () => {
  const failure = new ApiError("List unavailable", { kind: "network", url: "test/list" });
  const fetcher = vi.fn<() => Promise<string[]>>()
    .mockResolvedValueOnce(["retained"])
    .mockRejectedValueOnce(failure)
    .mockResolvedValueOnce([]);
  const { result } = renderHook(() => useApiResource(["refresh-test"], fetcher), { wrapper });
  await waitFor(() => expect(result.current.data).toEqual(["retained"]));
  await act(async () => { await expect(result.current.refresh()).rejects.toBe(failure); });
  expect(result.current.data).toEqual(["retained"]);
  expect(result.current.error).toBe(failure);
  expect(result.current.isEmpty).toBe(false);
  await act(async () => { await result.current.refresh(); });
  expect(result.current.data).toEqual([]);
  expect(result.current.error).toBeUndefined();
  expect(result.current.isEmpty).toBe(true);
  expect(fetcher).toHaveBeenCalledTimes(3);
});

it("keeps refresh failures scoped to their resource key", async () => {
  const failure = new ApiError("Old list unavailable", { kind: "network", url: "test/list" });
  const fetcher = vi.fn<() => Promise<string[]>>()
    .mockResolvedValueOnce(["first"])
    .mockRejectedValueOnce(failure)
    .mockResolvedValueOnce(["second"]);
  const { result, rerender } = renderHook(({ id }) => useApiResource(["scoped-refresh", id], fetcher), { initialProps: { id: "first" }, wrapper });
  await waitFor(() => expect(result.current.data).toEqual(["first"]));
  await act(async () => { await expect(result.current.refresh()).rejects.toBe(failure); });
  rerender({ id: "second" });
  await waitFor(() => expect(result.current.data).toEqual(["second"]));
  expect(result.current.error).toBeUndefined();
});

it("does not read an idle resource when refresh is invoked", async () => {
  const fetcher = vi.fn(async () => ["unexpected"]);
  const { result } = renderHook(() => useApiResource(null, fetcher), { wrapper });
  await act(async () => { await result.current.refresh(); });
  expect(fetcher).not.toHaveBeenCalled();
  expect(result.current.data).toBeUndefined();
  expect(result.current.isLoading).toBe(false);
});

it("keeps an earlier refresh callback bound to its original resource after navigation", async () => {
  const fetcher = vi.fn(async (id: string) => [id]);
  const { result, rerender } = renderHook(({ id }) => useApiResource(["navigation-refresh", id], () => fetcher(id)), { initialProps: { id: "first" }, wrapper });
  await waitFor(() => expect(result.current.data).toEqual(["first"]));
  const firstRefresh = result.current.refresh;
  rerender({ id: "second" });
  await waitFor(() => expect(result.current.data).toEqual(["second"]));
  await act(async () => { await firstRefresh(); });
  expect(fetcher.mock.calls).toEqual([["first"], ["second"]]);
  expect(result.current.data).toEqual(["second"]);
  expect(result.current.error).toBeUndefined();
});
