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

it("shows permission denial on the affected inventory panels", async () => {
  setApiTransport(createRouterTransport(({ service }) => service(SystemService, {
    listNamespaces: () => ({}),
    getSubstrateSummary: () => ({}),
    listSubstrateActors: () => { throw new ConnectError("Not authorized", Code.PermissionDenied); },
    listSubstrateWorkers: () => { throw new ConnectError("Not authorized", Code.PermissionDenied); },
  })));
  renderInventory();

  for (const noun of ["actors", "workers"]) {
    const alert = await screen.findByTestId(`substrate-${noun}-error`);
    expect(alert).toHaveTextContent(`You do not have access to ${noun}`);
    expect(alert).not.toHaveTextContent("Not authorized");
    expect(within(alert).queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(screen.queryByText(`No ${noun} visible in this scope.`)).not.toBeInTheDocument();
  }
  expect(screen.getByText("No worker pools visible in this scope.")).toBeInTheDocument();
  expect(screen.getByText("No actor templates visible in this scope.")).toBeInTheDocument();
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
    const actorsError = await screen.findByTestId("substrate-actors-error");
    expect(actorsError).toHaveTextContent("Actor read failed");
    expect(within(actorsError).getByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(await screen.findByTestId("substrate-workers-error")).toHaveTextContent("Worker read failed");
  },
);

it("hides the rows and both pagers when a refresh denies access on page two", async () => {
  let denied = false;
  setApiTransport(createRouterTransport(({ service }) => service(SystemService, {
    listNamespaces: () => ({}),
    getSubstrateSummary: () => ({}),
    listSubstrateActors: ({ page }) => {
      if (denied) throw new ConnectError("Actor inventory access denied", Code.PermissionDenied);
      return {
        actors: [{ metadata: { atespace: "team", name: page?.pageToken ? "second-actor" : "first-actor" } }],
        page: { nextPageToken: page?.pageToken ? "" : "actor-next" },
      };
    },
    listSubstrateWorkers: ({ page }) => {
      if (denied) throw new ConnectError("Worker inventory access denied", Code.PermissionDenied);
      return {
        workers: [{ metadata: { name: "visible-worker" }, workerNamespace: "workers", workerPod: page?.pageToken ? "second-pod" : "first-pod" }],
        page: { nextPageToken: page?.pageToken ? "" : "worker-next" },
      };
    },
  })));
  renderInventory();
  for (const resource of ["actors", "workers"]) {
    const next = await screen.findByTestId(`substrate-${resource}-pages-next`);
    await waitFor(() => expect(next).toBeEnabled());
    fireEvent.click(next);
    await waitFor(() => expect(screen.getByTestId(`substrate-${resource}-pages-prev`)).toBeEnabled());
  }
  expect(await screen.findByText("second-actor")).toBeInTheDocument();
  expect(await screen.findByText("second-pod")).toBeInTheDocument();

  denied = true;
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(await screen.findByTestId("substrate-actors-error")).toHaveTextContent("You do not have access to actors");
  expect(await screen.findByTestId("substrate-workers-error")).toHaveTextContent("You do not have access to workers");
  expect(screen.queryByText("second-actor")).not.toBeInTheDocument();
  expect(screen.queryByText("second-pod")).not.toBeInTheDocument();
  expect(screen.queryByTestId("substrate-actors-pages")).not.toBeInTheDocument();
  expect(screen.queryByTestId("substrate-workers-pages")).not.toBeInTheDocument();
});
