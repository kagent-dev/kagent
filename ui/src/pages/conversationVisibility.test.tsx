import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { AgentService } from "@/generated/kagent/api/v1/agents_pb";
import { SessionService } from "@/generated/kagent/api/v1/sessions_pb";
import { SystemService } from "@/generated/kagent/api/v1/system_pb";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { AgentsTab } from "./AgentsPage";
import { UnmappedConversationsPage } from "./UnmappedConversationsPage";

function renderPage(page: "agents" | "unmapped", empty = false) {
  setApiTransport(createRouterTransport(({ service }) => {
    service(SystemService, { listNamespaces: () => ({ namespaces: [{ name: "team-a" }] }) });
    service(AgentService, { listAgents: () => ({ agents: [{
      ref: { namespace: "team-a", name: "assistant" },
      resource: { apiVersion: "api.kagent.dev/v1alpha3", kind: "Agent", value: {
        metadata: { namespace: "team-a", name: "assistant" },
        spec: { template: { inline: {} }, harnessRef: { name: "runner" } },
      } },
    }] }) });
    service(SessionService, { listSessions: (request) => {
      if (request.allCreators) throw new ConnectError("Broad refused", Code.PermissionDenied);
      return { sessions: empty ? [] : [{ id: "owned", creator: "alice", agent: { namespace: "team-a", name: page === "agents" ? "assistant" : "deleted" } }] };
    } });
  }));
  render(<ThemeProvider theme={themeFor("dark")}>
    <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
      <MemoryRouter>{page === "agents" ? <AgentsTab /> : <UnmappedConversationsPage />}</MemoryRouter>
    </SWRConfig>
  </ThemeProvider>);
}

afterEach(() => setApiTransport(undefined));

it("labels Agents counts as owned after a refused broad read", async () => {
  renderPage("agents");
  expect(await screen.findByTestId("agents-owned-conversations")).toHaveTextContent("Showing your conversations only");
  expect(await screen.findByTestId("agent-conversations-team-a/assistant")).toHaveTextContent("1 conversation");
  expect(screen.getByRole("columnheader", { name: /Your conversations/ })).toBeInTheDocument();
  expect(screen.queryByTestId("agents-counts-error")).not.toBeInTheDocument();
});

it("keeps a zero owned count scoped to the caller", async () => {
  renderPage("agents", true);
  await screen.findByTestId("agents-owned-conversations");
  expect(await screen.findByTestId("agent-conversations-team-a/assistant")).toHaveTextContent("0 conversations");
});

it("shows owned unmapped conversations after a refused broad read", async () => {
  renderPage("unmapped");
  expect(await screen.findByTestId("unmapped-owned-conversations")).toHaveTextContent("Showing your unmapped conversations only");
  expect(await screen.findByTestId("unmapped-link-owned")).toBeInTheDocument();
  expect(screen.queryByTestId("unmapped-error")).not.toBeInTheDocument();
});

it("does not describe an empty owned list as an installation-wide result", async () => {
  renderPage("unmapped", true);
  await screen.findByTestId("unmapped-owned-conversations");
  expect(await screen.findByText("None of your conversations are unmapped.")).toBeInTheDocument();
});
