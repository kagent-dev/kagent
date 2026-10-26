import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, expect, it } from "vitest";
import { createRouterTransport } from "@connectrpc/connect";
import { SystemService } from "@/generated/kagent/api/v1/system_pb";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { SubstratePage } from "./SubstratePage";

afterEach(() => setApiTransport(undefined));

it("keeps Next available after authorization filters an entire inventory page", async () => {
  const actorTokens: string[] = [];
  const workerTokens: string[] = [];
  setApiTransport(createRouterTransport(({ service }) => service(SystemService, {
    listNamespaces: () => ({}),
    getSubstrateSummary: () => ({ actorCount: 1n, workerCount: 1n }),
    listSubstrateActors: (request) => {
      const token = request.page?.pageToken ?? "";
      actorTokens.push(token);
      return token
        ? { actors: [{ metadata: { atespace: "team", name: "visible-actor" } }] }
        : { page: { nextPageToken: "actor-next" } };
    },
    listSubstrateWorkers: (request) => {
      const token = request.page?.pageToken ?? "";
      workerTokens.push(token);
      return token
        ? { workers: [{ metadata: { name: "visible-worker" }, workerNamespace: "workers", workerPod: "visible-pod" }] }
        : { page: { nextPageToken: "worker-next" } };
    },
  })));
  render(<ThemeProvider theme={themeFor("dark")}>
    <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
      <MemoryRouter><SubstratePage /></MemoryRouter>
    </SWRConfig>
  </ThemeProvider>);
  const nextActors = await screen.findByTestId("substrate-actors-pages-next");
  const nextWorkers = await screen.findByTestId("substrate-workers-pages-next");
  await waitFor(() => expect(nextActors).toBeEnabled());
  await waitFor(() => expect(nextWorkers).toBeEnabled());
  fireEvent.click(nextActors);
  fireEvent.click(nextWorkers);
  await screen.findByText("visible-actor");
  await screen.findByText("visible-pod");
  expect(actorTokens).toEqual(["", "actor-next"]);
  expect(workerTokens).toEqual(["", "worker-next"]);
  expect(screen.getByText(/Counts include only resources you can view/)).toBeInTheDocument();
  expect(screen.getByTestId("substrate-stat-scope")).toHaveTextContent("Requested filters");
});
