import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { CheckpointService, CheckpointState } from "@/generated/kagent/api/v1/checkpoints_pb";
import { setApiTransport } from "../transport";
import { useCheckpoints } from "./useCheckpoints";
import { deferred } from "@/testUtils/deferred";

function wrapper({ children }: { children: ReactNode }) {
  return <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false, dedupingInterval: 0 }}>{children}</SWRConfig>;
}

afterEach(() => setApiTransport(undefined));

it("shares confirmed deletion with readers while keeping other conversations and read failures intact", async () => {
  let unavailable = false;
  const requests: string[] = [];
  setApiTransport(createRouterTransport(({ service }) => service(CheckpointService, {
    listCheckpoints(request) {
      requests.push(request.sessionId);
      if (unavailable) throw new ConnectError("List unavailable", Code.Unavailable);
      return { checkpoints: [
        { id: `${request.sessionId}-ready`, state: CheckpointState.READY },
        { id: `${request.sessionId}-failed`, state: CheckpointState.FAILED },
      ] };
    },
  })));
  const { result } = renderHook(() => ({ first: useCheckpoints("first"), shared: useCheckpoints("first"), other: useCheckpoints("other") }), { wrapper });
  await waitFor(() => expect(result.current.first.data).toHaveLength(2));
  await waitFor(() => expect(result.current.other.data).toHaveLength(2));
  const reads = requests.length;
  await act(async () => { await result.current.first.forget("first-failed"); });
  expect(result.current.first.data?.map(row => row.id)).toEqual(["first-ready"]);
  expect(result.current.shared.data?.map(row => row.id)).toEqual(["first-ready"]);
  expect(result.current.other.data?.map(row => row.id)).toEqual(["other-ready", "other-failed"]);
  expect(requests).toHaveLength(reads);
  unavailable = true;
  await act(async () => { await expect(result.current.first.refresh()).rejects.toMatchObject({ code: "Unavailable" }); });
  expect(result.current.first.error?.code).toBe("Unavailable");
  expect(result.current.shared.error?.code).toBe("Unavailable");
  expect(result.current.first.data?.map(row => row.id)).toEqual(["first-ready"]);
  expect(result.current.other.error).toBeUndefined();
});

it.each(["success", "failure"] as const)("does not restore a deleted record or obsolete error from an earlier %s", async (outcome) => {
  const pending = deferred<{ checkpoints: Array<{ id: string; state: CheckpointState }> }>();
  const records = [{ id: "ready", state: CheckpointState.READY }, { id: "failed", state: CheckpointState.FAILED }];
  let reads = 0;
  setApiTransport(createRouterTransport(({ service }) => service(CheckpointService, {
    listCheckpoints() { return ++reads === 2 ? pending.promise : { checkpoints: records }; },
  })));
  const { result } = renderHook(() => useCheckpoints("conversation"), { wrapper });
  await waitFor(() => expect(result.current.data).toHaveLength(2));
  let earlier!: Promise<unknown>;
  act(() => { earlier = result.current.refresh().catch(error => error); });
  await waitFor(() => expect(reads).toBe(2));
  await act(async () => { await result.current.forget("failed"); });
  await act(async () => {
    if (outcome === "success") pending.resolve({ checkpoints: records });
    else pending.reject(new ConnectError("Obsolete read failure", Code.Unavailable));
    await earlier;
  });
  expect(result.current.data?.map(row => row.id)).toEqual(["ready"]);
  expect(result.current.error).toBeUndefined();
});
