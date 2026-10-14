import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, expect, it } from "vitest";
import { createRouterTransport } from "@connectrpc/connect";
import { AgentService } from "@/generated/kagent/api/v1alpha1/agents_pb";
import { SystemService } from "@/generated/kagent/api/v1alpha1/system_pb";
import { useAgentsAcrossNamespaces } from "@/api";
import { wrap } from "@/api/grpc/wire";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { DashboardPage } from "./DashboardPage";

afterEach(() => setApiTransport(undefined));

/** As many namespaces as a shared installation watches, each holding one Agent. */
const NAMESPACES = Array.from({ length: 35 }, (_, index) => `team-${index}`);

function agent(namespace: string) {
  return {
    ref: { namespace, name: "assistant" },
    resource: wrap("Agent", {
      metadata: { namespace, name: "assistant" },
      spec: { template: { systemPrompt: "help" }, harnessRef: { name: "runtime" } },
      status: { latestSuccessfulRevision: "r1", desiredRevision: "r1" },
    }),
  };
}

/** A controller that answers namespaces and agents, recording each ListAgents namespace. */
function controller(listed: string[]) {
  return createRouterTransport(({ service }) => {
    service(SystemService, {
      listNamespaces: () => ({ namespaces: NAMESPACES.map((name) => ({ name })) }),
    });
    service(AgentService, {
      listAgents: (request) => {
        listed.push(request.namespace);
        return {
          agents: NAMESPACES.filter((name) => !request.namespace || name === request.namespace).map(agent),
        };
      },
    });
  });
}

/** A second reader on the same screen, as the agent switcher is, naming fewer namespaces. */
function SecondReader() {
  const agents = useAgentsAcrossNamespaces(NAMESPACES.slice(0, 2));
  return <span data-testid="second-reader">{agents.data?.agents.length ?? "-"}</span>;
}

it("reads the agent list once per load and once per Refresh, however many namespaces", async () => {
  const listed: string[] = [];
  setApiTransport(controller(listed));
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
        <MemoryRouter>
          <DashboardPage />
          <SecondReader />
        </MemoryRouter>
      </SWRConfig>
    </ThemeProvider>,
  );

  // Both readers see their own namespaces out of the one cluster-wide read.
  await waitFor(() => expect(screen.getByTestId("second-reader")).toHaveTextContent("2"));
  await screen.findByText(`${NAMESPACES.length} ready`);
  expect(listed).toEqual([""]);

  fireEvent.click(screen.getByTestId("refresh-button"));
  await waitFor(() => expect(listed).toEqual(["", ""]));
  // The Agents count is still the whole cluster's after the Refresh.
  await screen.findByText(`${NAMESPACES.length} ready`);
});
