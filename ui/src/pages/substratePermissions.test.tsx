import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { SystemService } from "@/generated/kagent/api/v1/system_pb";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { SubstratePage } from "@/pages/SubstratePage";

afterEach(() => setApiTransport(undefined));

function renderInventory() {
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
        <MemoryRouter><SubstratePage /></MemoryRouter>
      </SWRConfig>
    </ThemeProvider>,
  );
}

it("shows consistent empty tables when inventory access is denied", async () => {
  let actorReads = 0;
  let workerReads = 0;
  setApiTransport(createRouterTransport(({ service }) => service(SystemService, {
    listNamespaces: () => ({}),
    getSubstrateSummary: () => ({}),
    listSubstrateActors: () => {
      actorReads++;
      throw new ConnectError("Actor inventory access denied", Code.PermissionDenied);
    },
    listSubstrateWorkers: () => {
      workerReads++;
      throw new ConnectError("Worker inventory access denied", Code.PermissionDenied);
    },
  })));
  renderInventory();
  await waitFor(() => {
    expect(actorReads).toBeGreaterThan(0);
    expect(workerReads).toBeGreaterThan(0);
    expect(screen.queryByTestId("substrate-actors-pages-next")).not.toBeInTheDocument();
    expect(screen.queryByTestId("substrate-workers-pages-next")).not.toBeInTheDocument();
  });
  for (const [table, noun] of [["pools", "worker pools"], ["templates", "actor templates"], ["actors", "actors"], ["workers", "workers"]]) {
    expect(within(screen.getByTestId(`substrate-${table}-table`)).getByText(`No ${noun} visible in this scope.`)).toBeInTheDocument();
  }
  expect(screen.queryByTestId("substrate-actors-error")).not.toBeInTheDocument();
  expect(screen.queryByTestId("substrate-workers-error")).not.toBeInTheDocument();
});

it.each([Code.Unauthenticated, Code.Internal, Code.Unavailable, Code.DeadlineExceeded])(
  "keeps inventory failures visible for gRPC code %s",
  async (code) => {
    setApiTransport(createRouterTransport(({ service }) => service(SystemService, {
      listNamespaces: () => ({}),
      getSubstrateSummary: () => ({}),
      listSubstrateActors: () => { throw new ConnectError("Actor read failed", code); },
      listSubstrateWorkers: () => { throw new ConnectError("Worker read failed", code); },
    })));
    renderInventory();
    expect(await screen.findByTestId("substrate-actors-error")).toHaveTextContent("Actor read failed");
    expect(await screen.findByTestId("substrate-workers-error")).toHaveTextContent("Worker read failed");
  },
);

it("clears previously visible rows and continuation tokens when refresh denies access", async () => {
  let denied = false;
  setApiTransport(createRouterTransport(({ service }) => service(SystemService, {
    listNamespaces: () => ({}),
    getSubstrateSummary: () => ({}),
    listSubstrateActors: () => {
      if (denied) throw new ConnectError("Actor inventory access denied", Code.PermissionDenied);
      return { actors: [{ metadata: { atespace: "team", name: "visible-actor" } }], page: { nextPageToken: "actor-next" } };
    },
    listSubstrateWorkers: () => {
      if (denied) throw new ConnectError("Worker inventory access denied", Code.PermissionDenied);
      return { workers: [{ metadata: { name: "visible-worker" }, workerNamespace: "workers", workerPod: "visible-pod" }], page: { nextPageToken: "worker-next" } };
    },
  })));
  renderInventory();
  await screen.findByText("visible-actor");
  await screen.findByText("visible-pod");
  await waitFor(() => expect(screen.getByTestId("substrate-workers-pages-next")).toBeEnabled());
  denied = true;
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => {
    expect(screen.queryByText("visible-actor")).not.toBeInTheDocument();
    expect(screen.queryByText("visible-pod")).not.toBeInTheDocument();
    expect(screen.queryByTestId("substrate-actors-pages-next")).not.toBeInTheDocument();
    expect(screen.queryByTestId("substrate-workers-pages-next")).not.toBeInTheDocument();
  });
  expect(screen.queryByTestId("substrate-actors-error")).not.toBeInTheDocument();
  expect(screen.queryByTestId("substrate-workers-error")).not.toBeInTheDocument();
});
