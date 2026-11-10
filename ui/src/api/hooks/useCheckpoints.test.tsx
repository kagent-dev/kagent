import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { CheckpointService, CheckpointState } from "@/generated/kagent/api/v1/checkpoints_pb";
import { setApiTransport } from "../transport";
import { useCheckpoints } from "./useCheckpoints";

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
