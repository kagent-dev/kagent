import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { SWRConfig } from "swr";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { AgentService } from "@/generated/kagent/api/v1/agents_pb";
import { AgentTemplateService } from "@/generated/kagent/api/v1/agent_templates_pb";
import { SessionService } from "@/generated/kagent/api/v1/sessions_pb";
import { SystemService } from "@/generated/kagent/api/v1/system_pb";
import { setApiTransport } from "@/api/transport";
import { themeFor } from "@/theme/theme";
import { AgentsTab } from "./AgentsPage";
import { UnmappedConversationsPage } from "./UnmappedConversationsPage";

function gate() {
  let release!: () => void;
  const promise = new Promise<void>((resolve) => { release = resolve; });
  return { promise, release: () => act(async () => { release(); await promise; }) };
}

function backend({
  namespacesReady = Promise.resolve(),
  agentsReady = Promise.resolve(),
  templatesReady = Promise.resolve(),
  conversationsReady = Promise.resolve(),
  emptyNamespaces = false,
}: {
  namespacesReady?: Promise<void>;
  agentsReady?: Promise<void>;
  templatesReady?: Promise<void>;
  conversationsReady?: Promise<void>;
  emptyNamespaces?: boolean;
} = {}) {
  const listNamespaces = vi.fn(async () => {
    await namespacesReady;
    return { namespaces: emptyNamespaces ? [] : [{ name: "team-a" }] };
  });
  const listAgents = vi.fn(async () => {
    await agentsReady;
    return { agents: [{
      ref: { namespace: "team-a", name: "assistant" },
      resource: { apiVersion: "api.kagent.dev/v1alpha3", kind: "Agent", value: {
        metadata: { namespace: "team-a", name: "assistant" },
        spec: { template: { ref: { name: "shared" } }, harnessRef: { name: "runner" } },
      } },
    }] };
  });
  const listTemplates = vi.fn(async () => {
    await templatesReady;
    return { agentTemplates: [] };
  });
  const listSessions = vi.fn(async () => {
    await conversationsReady;
    return { sessions: [
      { id: "mapped", name: "Mapped conversation", agent: { namespace: "team-a", name: "assistant" } },
      { id: "orphan", name: "Orphan conversation", agent: { namespace: "team-a", name: "deleted" } },
    ] };
  });
  setApiTransport(createRouterTransport(({ service }) => {
    service(SystemService, { listNamespaces });
    service(AgentService, { listAgents });
    service(AgentTemplateService, { listAgentTemplates: listTemplates });
    service(SessionService, { listSessions });
  }));
  return { listNamespaces, listAgents, listTemplates, listSessions };
}

function renderPage(page: "agents" | "unmapped", search = "") {
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <SWRConfig value={{ provider: () => new Map(), shouldRetryOnError: false }}>
        <MemoryRouter initialEntries={[`/agents${search}`]}>
          {page === "agents" ? <AgentsTab /> : <UnmappedConversationsPage />}
        </MemoryRouter>
      </SWRConfig>
    </ThemeProvider>,
  );
}

afterEach(() => setApiTransport(undefined));

describe.each(["agents", "unmapped"] as const)("%s namespace loading", (page) => {
  it("keeps a skeleton until namespaces and agents arrive, without claiming conversations are unmapped", async () => {
    const namespaces = gate();
    const agents = gate();
    const calls = backend({ namespacesReady: namespaces.promise, agentsReady: agents.promise });
    renderPage(page);

    await waitFor(() => expect(calls.listSessions).toHaveBeenCalled());
    expect(screen.getByTestId(`${page}-loading`).querySelector(".ant-skeleton-active")).not.toBeNull();
    expect(calls.listAgents).not.toHaveBeenCalled();
    expect(screen.queryByTestId("agents-summary")).not.toBeInTheDocument();
    expect(screen.queryByTestId("unmapped-link-mapped")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Delete conversations all/ })).not.toBeInTheDocument();

    await namespaces.release();
    await waitFor(() => expect(calls.listAgents).toHaveBeenCalledOnce());
    expect(screen.getByTestId(`${page}-loading`)).toBeInTheDocument();
    expect(screen.queryByTestId("unmapped-link-mapped")).not.toBeInTheDocument();

    await agents.release();
    await waitFor(() => expect(screen.queryByTestId(`${page}-loading`)).not.toBeInTheDocument());
    if (page === "agents") {
      expect(await screen.findByTestId("agents-summary")).toHaveTextContent("1 of 1 agent");
      expect(screen.getByTestId("agent-link-team-a-assistant")).toBeInTheDocument();
    } else {
      expect(screen.getByTestId("unmapped-link-orphan")).toBeInTheDocument();
      expect(screen.queryByTestId("unmapped-link-mapped")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Delete conversations all/ })).toBeInTheDocument();
    }
  });

  it("reports a namespace failure instead of an empty list or a perpetual skeleton", async () => {
    const calls = backend({ namespacesReady: Promise.reject(new ConnectError("Namespaces unavailable", Code.Unavailable)) });
    renderPage(page);

    expect(await screen.findByTestId(`${page}-error`)).toHaveTextContent("Namespaces unavailable");
    expect(screen.queryByTestId(`${page}-loading`)).not.toBeInTheDocument();
    expect(calls.listAgents).not.toHaveBeenCalled();
    expect(screen.queryByText(/No agents yet|No visible conversations are unmapped/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Delete conversations all/ })).not.toBeInTheDocument();
  });

  it("settles a genuinely empty namespace list", async () => {
    const calls = backend({ emptyNamespaces: true });
    renderPage(page);

    await waitFor(() => expect(screen.queryByTestId(`${page}-loading`)).not.toBeInTheDocument());
    if (page === "agents") {
      expect(await screen.findByText("No agents yet. Create one to pair a template with a harness.")).toBeInTheDocument();
    } else {
      expect(await screen.findByTestId("unmapped-link-orphan")).toBeInTheDocument();
    }
    expect(calls.listAgents).not.toHaveBeenCalled();
    expect(screen.queryByTestId(`${page}-error`)).not.toBeInTheDocument();
  });

  it("stops the skeleton when the agent read fails, even if conversations are still loading", async () => {
    const conversations = gate();
    backend({
      agentsReady: Promise.reject(new ConnectError("Agents unavailable", Code.Unavailable)),
      conversationsReady: conversations.promise,
    });
    renderPage(page);

    expect(await screen.findByTestId(`${page}-error`)).toHaveTextContent("Agents unavailable");
    expect(screen.queryByTestId(`${page}-loading`)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Delete conversations all/ })).not.toBeInTheDocument();
    await conversations.release();
  });
});

it("retries failed namespaces before reading Agents", async () => {
  const calls = backend();
  calls.listNamespaces.mockRejectedValueOnce(new ConnectError("Namespaces unavailable", Code.Unavailable));
  renderPage("agents");
  await screen.findByTestId("agents-error");
  expect(calls.listAgents).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByTestId("agent-link-team-a-assistant")).toBeInTheDocument();
  expect(calls.listNamespaces).toHaveBeenCalledTimes(2);
  expect(calls.listAgents).toHaveBeenCalledOnce();
  expect(screen.queryByTestId("agents-error")).not.toBeInTheDocument();
});

it("waits for namespaces even when the Agents URL already selects one", async () => {
  const namespaces = gate();
  const calls = backend({ namespacesReady: namespaces.promise });
  renderPage("agents", "?ns=team-a");

  await waitFor(() => expect(calls.listSessions).toHaveBeenCalled());
  expect(calls.listAgents).not.toHaveBeenCalled();
  expect(screen.getByTestId("agents-loading")).toBeInTheDocument();
  await namespaces.release();
  expect(await screen.findByTestId("agent-link-team-a-assistant")).toBeInTheDocument();
});

it("keeps the Agents skeleton while referenced descriptions load", async () => {
  const templates = gate();
  const calls = backend({ templatesReady: templates.promise });
  renderPage("agents");

  await waitFor(() => expect(calls.listTemplates).toHaveBeenCalledOnce());
  expect(screen.getByTestId("agents-loading")).toBeInTheDocument();
  expect(screen.queryByTestId("agent-link-team-a-assistant")).not.toBeInTheDocument();
  await templates.release();
  expect(await screen.findByTestId("agent-link-team-a-assistant")).toBeInTheDocument();
});

it("keeps the Unmapped skeleton while conversations load", async () => {
  const conversations = gate();
  const calls = backend({ conversationsReady: conversations.promise });
  renderPage("unmapped");

  await waitFor(() => expect(calls.listAgents).toHaveBeenCalledOnce());
  expect(screen.getByTestId("unmapped-loading")).toBeInTheDocument();
  expect(screen.queryByText("No visible conversations are unmapped.")).not.toBeInTheDocument();
  await conversations.release();
  expect(await screen.findByTestId("unmapped-link-orphan")).toBeInTheDocument();
});
