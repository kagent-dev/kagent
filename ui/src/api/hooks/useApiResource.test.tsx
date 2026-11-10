import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig, useSWRConfig } from "swr";
import { expect, it, vi } from "vitest";
import { ApiError } from "../ApiError";
import { useApiResource } from "./useApiResource";
import { deferred } from "@/testUtils/deferred";

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

it.each(["refresh", "background"] as const)("ignores an older failure after a newer %s succeeds for all readers", async (mode) => {
  const older = deferred<string[]>();
  const failure = new ApiError("Earlier read failed", { kind: "network", url: "test/list" });
  const fetcher = vi.fn<() => Promise<string[]>>()
    .mockResolvedValueOnce(["initial"])
    .mockReturnValueOnce(older.promise)
    .mockResolvedValueOnce(["repaired"]);
  const { result } = renderHook(() => ({
    first: useApiResource(["ordered", mode], fetcher),
    shared: useApiResource(["ordered", mode], fetcher),
    revalidate: useSWRConfig().mutate,
  }), { wrapper });
  await waitFor(() => expect(result.current.first.data).toEqual(["initial"]));
  let earlier!: Promise<unknown>;
  act(() => { earlier = result.current.first.refresh().catch(error => error); });
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  await act(async () => {
    if (mode === "refresh") await result.current.first.refresh();
    else await result.current.revalidate(["ordered", mode]);
  });
  expect(result.current.first.data).toEqual(["repaired"]);
  await act(async () => { older.reject(failure); await earlier; });
  expect(result.current.first.error).toBeUndefined();
  expect(result.current.shared.error).toBeUndefined();
  expect(result.current.first.data).toEqual(["repaired"]);
});

it("preserves the newer failure when an older read fails afterward", async () => {
  const older = deferred<string[]>();
  const failure = new ApiError("Current read failed", { kind: "network", url: "test/list" });
  const fetcher = vi.fn<() => Promise<string[]>>()
    .mockResolvedValueOnce(["retained"])
    .mockReturnValueOnce(older.promise)
    .mockRejectedValueOnce(failure);
  const { result } = renderHook(() => useApiResource(["ordered-failure"], fetcher), { wrapper });
  await waitFor(() => expect(result.current.data).toEqual(["retained"]));
  let earlier!: Promise<unknown>;
  act(() => { earlier = result.current.refresh().catch(error => error); });
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  await act(async () => { await expect(result.current.refresh()).rejects.toBe(failure); });
  await act(async () => { older.reject(new ApiError("Obsolete failure", { kind: "network", url: "test/list" })); await earlier; });
  expect(result.current.error).toBe(failure);
  expect(result.current.data).toEqual(["retained"]);
});

it("keeps concurrent reads with the same key isolated across cache providers", async () => {
  const pending = deferred<string[]>();
  const failure = new ApiError("First provider failed", { kind: "network", url: "test/list" });
  const first = renderHook(() => useApiResource(["provider-isolation"], () => pending.promise), { wrapper });
  const second = renderHook(() => useApiResource(["provider-isolation"], async () => ["healthy"]), { wrapper });
  await waitFor(() => expect(second.result.current.data).toEqual(["healthy"]));
  await act(async () => { pending.reject(failure); });
  await waitFor(() => expect(first.result.current.error).toBe(failure));
  expect(second.result.current.data).toEqual(["healthy"]);
  expect(second.result.current.error).toBeUndefined();
});

it("reports a later current failure normally after overlapping reads have settled", async () => {
  const older = deferred<string[]>();
  const failure = new ApiError("New current failure", { kind: "network", url: "test/list" });
  const onError = vi.fn();
  const fetcher = vi.fn<() => Promise<string[]>>()
    .mockResolvedValueOnce(["initial"])
    .mockReturnValueOnce(older.promise)
    .mockResolvedValueOnce(["repaired"])
    .mockRejectedValueOnce(failure);
  const { result } = renderHook(() => useApiResource(["after-overlap"], fetcher, { onError }), { wrapper });
  await waitFor(() => expect(result.current.data).toEqual(["initial"]));
  let earlier!: Promise<unknown>;
  act(() => { earlier = result.current.refresh().catch(error => error); });
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  await act(async () => { await result.current.refresh(); });
  await act(async () => { older.reject(new ApiError("Obsolete failure", { kind: "network", url: "test/list" })); await earlier; });
  expect(onError).not.toHaveBeenCalled();
  await act(async () => { await expect(result.current.refresh()).rejects.toBe(failure); });
  expect(result.current.error).toBe(failure);
  expect(onError).toHaveBeenCalledTimes(1);
  expect(onError.mock.calls[0][0]).toBe(failure);
});
