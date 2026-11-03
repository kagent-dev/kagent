import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { SessionService } from "@/generated/kagent/api/v1/sessions_pb";
import { setApiTransport } from "../transport";
import { useAgentConversations, useAgentInstances } from "./useAgentInstances";

function wrapper({ children }: { children: ReactNode }) {
  return <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>{children}</SWRConfig>;
}

afterEach(() => setApiTransport(undefined));

describe.each(["collection", "agent"] as const)("%s conversations", (mode) => {
  const useConversations: () => ReturnType<typeof useAgentInstances> | ReturnType<typeof useAgentConversations> = mode === "collection"
    ? () => useAgentInstances(true)
    : () => useAgentConversations("team-a", "assistant");

  it("falls back after permission denial and follows every owned page", async () => {
    const requests: { allCreators: boolean; pageToken: string }[] = [];
    setApiTransport(createRouterTransport(({ service }) => service(SessionService, {
      listSessions: (request) => {
        requests.push({ allCreators: request.allCreators, pageToken: request.page?.pageToken ?? "" });
        expect(request.agent?.name).toBe(mode === "agent" ? "assistant" : undefined);
        if (request.allCreators) throw new ConnectError("Broad list refused", Code.PermissionDenied);
        if (!request.page?.pageToken) return { page: { nextPageToken: "next" } };
        return { sessions: [{ id: "owned", creator: "alice" }] };
      },
    })));
    const { result } = renderHook(useConversations, { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());
    const data = result.current.data;
    const rows = Array.isArray(data) ? data : data?.all;
    expect(rows?.map((row) => row.id)).toEqual(["owned"]);
    const refused = "widerReadRefused" in result.current
      ? result.current.widerReadRefused
      : !Array.isArray(data) && data?.widerReadRefused;
    expect(refused).toBe("Broad list refused");
    expect(requests).toEqual(mode === "collection" ? [
      { allCreators: true, pageToken: "" },
      { allCreators: false, pageToken: "" },
      { allCreators: false, pageToken: "next" },
    ] : [
      { allCreators: false, pageToken: "" },
      { allCreators: true, pageToken: "" },
      { allCreators: false, pageToken: "next" },
    ]);
  });

  it.each([Code.Unauthenticated, Code.Internal, Code.Unavailable])("propagates broad %s instead of displaying owned results", async (code) => {
    const requests: boolean[] = [];
    setApiTransport(createRouterTransport(({ service }) => service(SessionService, {
      listSessions: (request) => {
        requests.push(request.allCreators);
        if (mode === "agent" && !request.allCreators) return {};
        throw new ConnectError("Failed", code);
      },
    })));
    const { result } = renderHook(useConversations, { wrapper });
    await waitFor(() => expect(result.current.error).toBeDefined());
    expect(result.current.data).toBeUndefined();
    expect(requests).toEqual(mode === "collection" ? [true] : [false, true]);
  });

  it("preserves a denied owned read as an error", async () => {
    setApiTransport(createRouterTransport(({ service }) => service(SessionService, {
      listSessions: (request) => {
        throw new ConnectError(request.allCreators ? "Broad refused" : "Owned refused", Code.PermissionDenied);
      },
    })));
    const { result } = renderHook(useConversations, { wrapper });
    await waitFor(() => expect(result.current.error?.message).toBe("Owned refused"));
    expect(result.current.data).toBeUndefined();
    expect(result.current.isEmpty).toBe(false);
  });

  it("marks an empty owned fallback without claiming broad visibility", async () => {
    setApiTransport(createRouterTransport(({ service }) => service(SessionService, {
      listSessions: (request) => {
        if (request.allCreators) throw new ConnectError("Broad refused", Code.PermissionDenied);
        return {};
      },
    })));
    const { result } = renderHook(useConversations, { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.error).toBeUndefined();
    if (mode === "collection") expect(result.current.isEmpty).toBe(true);
    else expect(result.current.data).toMatchObject({ all: [], own: [], widerReadRefused: "Broad refused" });
  });
});

it("keeps broad metadata separate from the owned list on agent details", async () => {
  setApiTransport(createRouterTransport(({ service }) => service(SessionService, {
    listSessions: (request) => ({ sessions: request.allCreators
      ? [{ id: "other", creator: "bob" }, { id: "owned", creator: "alice" }]
      : [{ id: "owned", creator: "alice" }] }),
  })));
  const { result } = renderHook(() => useAgentConversations("team-a", "assistant"), { wrapper });
  await waitFor(() => expect(result.current.data?.all).toHaveLength(2));
  expect(result.current.data?.own.map((row) => row.id)).toEqual(["owned"]);
  expect(result.current.data?.widerReadRefused).toBeUndefined();
});

it("loads owned conversations while the broad collection is still pending", async () => {
  let finishBroad!: () => void;
  const broadPending = new Promise<void>((resolve) => { finishBroad = resolve; });
  const requests: boolean[] = [];
  setApiTransport(createRouterTransport(({ service }) => service(SessionService, {
    listSessions: async (request) => {
      requests.push(request.allCreators);
      if (request.allCreators) await broadPending;
      return { sessions: [{ id: request.allCreators ? "other" : "owned", creator: "alice" }] };
    },
  })));
  const { result } = renderHook(() => useAgentConversations("team-a", "assistant"), { wrapper });
  try {
    await waitFor(() => expect(requests).toEqual([false, true]));
    expect(result.current.data).toBeUndefined();
  } finally {
    finishBroad();
  }
  await waitFor(() => expect(result.current.data?.own.map((row) => row.id)).toEqual(["owned"]));
  expect(result.current.data?.all.map((row) => row.id)).toEqual(["other"]);
});
