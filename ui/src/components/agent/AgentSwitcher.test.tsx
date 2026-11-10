import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Agent, ApiResource, NamespaceResponse } from "@/api";
import { ApiError } from "@/api/ApiError";
import { themeFor } from "@/theme/theme";
import { AgentSwitcher } from "./AgentSwitcher";

const { useNamespaces, useAgentsAcrossNamespaces } = vi.hoisted(() => ({
  useNamespaces: vi.fn(),
  useAgentsAcrossNamespaces: vi.fn(),
}));

vi.mock("@/api", () => ({
  useNamespaces,
  useAgentsAcrossNamespaces,
  harnessRefName: (agent: Agent) => agent.resource.spec.harnessRef.name,
}));

type Definitions = { agents: Agent[]; refused: { namespace: string; reason: string }[] };

const namespaces: NamespaceResponse[] = [{ name: "kagent", status: "Active" }];

function agent(namespace: string, name: string, harness: string): Agent {
  return {
    ref: `${namespace}/${name}`,
    namespace,
    name,
    resource: {
      metadata: { name },
      spec: { template: { ref: { name: "assistant" } }, harnessRef: { name: harness } },
    },
  };
}

const CURRENT = agent("kagent", "k8s", "default");
const OTHER = agent("kagent", "reporting", "default");

function loaded<T>(data: T): ApiResource<T> {
  return {
    data,
    isLoading: false,
    isValidating: false,
    error: undefined,
    isEmpty: Array.isArray(data) ? data.length === 0 : false,
    refresh: async () => {},
  };
}

function reading<T>(): ApiResource<T> {
  return {
    data: undefined,
    isLoading: true,
    isValidating: true,
    error: undefined,
    isEmpty: false,
    refresh: async () => {},
  };
}

/** A read held back because its key is not ready: SWR reports this with isLoading false. */
function held<T>(): ApiResource<T> {
  return {
    data: undefined,
    isLoading: false,
    isValidating: false,
    error: undefined,
    isEmpty: false,
    refresh: async () => {},
  };
}

function failing<T>(message: string): ApiResource<T> {
  return {
    ...held<T>(),
    error: new ApiError(message, { kind: "http", url: "kagent.test/Read", status: 500 }),
  };
}

function renderSwitcher() {
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <MemoryRouter>
        <AgentSwitcher current={{ namespace: "kagent", name: "k8s" }} onPicked={() => {}} />
      </MemoryRouter>
    </ThemeProvider>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("AgentSwitcher", () => {
  it("holds the agents read back until the namespaces are known", () => {
    useNamespaces.mockReturnValue(reading<NamespaceResponse[]>());
    useAgentsAcrossNamespaces.mockReturnValue(held<Definitions>());

    renderSwitcher();

    expect(useAgentsAcrossNamespaces).toHaveBeenCalledWith(undefined);
  });

  it("shows a placeholder while the namespaces are read, not an empty cluster", () => {
    useNamespaces.mockReturnValue(reading<NamespaceResponse[]>());
    useAgentsAcrossNamespaces.mockReturnValue(held<Definitions>());

    renderSwitcher();

    const placeholders = screen.getAllByTestId("agent-switcher-loading");
    expect(placeholders.length).toBeGreaterThan(0);
    // Visible, not just height: the placeholder used to be painted in the panel's own
    // colour, so it held the shape and still read as an empty panel.
    expect(placeholders[0].querySelector(".ant-skeleton-active")).not.toBeNull();
    expect(screen.queryByTestId("agent-switcher-empty")).toBeNull();
  });

  it("keeps the placeholder while the agents are read", () => {
    useNamespaces.mockReturnValue(loaded(namespaces));
    useAgentsAcrossNamespaces.mockReturnValue(reading<Definitions>());

    renderSwitcher();

    expect(screen.getAllByTestId("agent-switcher-loading").length).toBeGreaterThan(0);
    expect(screen.queryByTestId("agent-switcher-empty")).toBeNull();
  });

  it("lists the other agents once both reads land, without the one you are on", () => {
    useNamespaces.mockReturnValue(loaded(namespaces));
    useAgentsAcrossNamespaces.mockReturnValue(
      loaded<Definitions>({ agents: [CURRENT, OTHER], refused: [] }),
    );

    renderSwitcher();

    expect(screen.getByTestId(`agent-switcher-option-${OTHER.name}`)).toBeInTheDocument();
    expect(screen.queryByTestId(`agent-switcher-option-${CURRENT.name}`)).toBeNull();
    expect(screen.queryByTestId("agent-switcher-loading")).toBeNull();
    expect(screen.queryByTestId("agent-switcher-empty")).toBeNull();
  });

  it("says the cluster is empty only after both reads have said so", () => {
    useNamespaces.mockReturnValue(loaded(namespaces));
    useAgentsAcrossNamespaces.mockReturnValue(loaded<Definitions>({ agents: [], refused: [] }));

    renderSwitcher();

    expect(screen.getByTestId("agent-switcher-empty")).toHaveTextContent(
      "No other agents on this cluster.",
    );
    expect(screen.queryByTestId("agent-switcher-loading")).toBeNull();
  });

  it("reports a failed agents read as a failure, not as an empty cluster", () => {
    useNamespaces.mockReturnValue(loaded(namespaces));
    useAgentsAcrossNamespaces.mockReturnValue(failing<Definitions>("the read failed"));

    renderSwitcher();

    expect(screen.getByTestId("agent-switcher-error")).toHaveTextContent(
      "Could not list agents. the read failed",
    );
    expect(screen.queryByTestId("agent-switcher-empty")).toBeNull();
  });

  it("reports a failed namespaces read rather than an empty cluster", () => {
    useNamespaces.mockReturnValue(failing<NamespaceResponse[]>("no namespaces"));
    useAgentsAcrossNamespaces.mockReturnValue(held<Definitions>());

    renderSwitcher();

    expect(screen.getByTestId("agent-switcher-error")).toHaveTextContent(
      "Could not list agents. no namespaces",
    );
    expect(screen.queryByTestId("agent-switcher-empty")).toBeNull();
  });
});
