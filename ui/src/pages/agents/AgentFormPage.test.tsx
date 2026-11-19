import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { AgentFormPage } from "./AgentFormPage";
import { FIX_FIELDS_MESSAGE } from "@/components/form/useSubmitValidation";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  success: vi.fn(),
  agent: {
    namespace: "kagent",
    name: "helper",
    resource: {
      metadata: { namespace: "kagent", name: "helper", resourceVersion: "3" },
      spec: { template: { ref: { name: "shared-brain" } }, harnessRef: { name: "kagent-default" } },
    },
  },
}));

const list = <T,>(data: T[]) => ({ data, isLoading: false, error: undefined });

vi.mock("react-hot-toast", () => ({ default: { success: mocks.success } }));
vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { agentBuildingBlocks: { createAgent: mocks.create, updateAgent: mocks.update } },
  useAgent: (namespace?: string, name?: string) =>
    namespace && name ? { data: mocks.agent, error: undefined } : { data: undefined, error: undefined },
  useAgentTemplates: () => list([{ name: "shared-brain", modelConfigRef: "kagent/default-model-config" }, { name: "other" }]),
  useHarnesses: () => list([{ name: "kagent-default", runtime: "kagent" }, { name: "fast-lane", runtime: "codex" }]),
  useNamespaceEgressPolicies: () => list([]),
  useInvalidateAgents: () => () => Promise.resolve(),
  useModel: () => ({ data: undefined, error: undefined }),
  useModels: () => list([]),
  useMcpServers: () => list([]),
  useTools: () => list([]),
  useNamespaces: () => list([{ name: "kagent" }, { name: "team-a" }]),
}));

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      { path: "/agents", element: <p>Agent list</p> },
      { path: "/agents/new", element: <AgentFormPage /> },
      { path: "/agents/:namespace/:name/edit", element: <AgentFormPage /> },
    ],
    { initialEntries: [path] },
  );
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <RouterProvider router={router} />
      </ConfigProvider>
    </ThemeProvider>,
  );
  return router;
}

// An input event, as typing sends: it also marks the field touched.
const type = (testId: string, value: string) =>
  fireEvent.input(screen.getByTestId(testId), { target: { value } });

function choose(testId: string, option: string) {
  fireEvent.mouseDown(within(screen.getByTestId(testId)).getByRole("combobox"));
  fireEvent.click(screen.getByTitle(option));
}

beforeEach(() => vi.clearAllMocks());

describe("agent create form", () => {
  it("shows a back link and marks every field required or optional", () => {
    renderAt("/agents/new");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Agents");
    // Name, Namespace, Harness, Agent template.
    expect(screen.getAllByTestId("field-required")).toHaveLength(4);
    // Egress policies.
    expect(screen.getAllByTestId("field-optional")).toHaveLength(1);
    expect(screen.getByText("Use existing")).toBeInTheDocument();
    expect(screen.getByText("Define here")).toBeInTheDocument();
  });

  it("picks the namespace, harness and template from dropdowns", () => {
    renderAt("/agents/new");
    for (const id of ["agent-form-namespace", "agent-form-harness-ref", "agent-form-template-ref"]) {
      expect(screen.getByTestId(id)).toHaveClass("ant-select");
    }
    expect(screen.getByTestId("agent-form-harness-ref")).toHaveTextContent("kagent-default");
  });

  it("holds Create back until the form is valid, and flags a field once it is left", () => {
    renderAt("/agents/new");
    const submit = screen.getByTestId("agent-form-submit");
    expect(submit).toHaveAttribute("aria-disabled", "true");
    expect(submit).toHaveAccessibleDescription(FIX_FIELDS_MESSAGE);
    expect(screen.queryByText("A name is required.")).not.toBeInTheDocument();

    fireEvent.click(submit);
    expect(mocks.create).not.toHaveBeenCalled();
    expect(screen.queryByText("A name is required.")).not.toBeInTheDocument();

    fireEvent.blur(screen.getByTestId("agent-form-name"));
    expect(screen.getByTestId("agent-form-name")).toHaveAccessibleDescription(/A name is required\.$/);
    expect(screen.queryByText("Choose an agent template.")).not.toBeInTheDocument();

    type("agent-form-name", "helper");
    choose("agent-form-template-ref", "shared-brain");
    expect(submit).not.toHaveAttribute("aria-disabled");
    expect(screen.queryByTestId("agent-form-blocked")).not.toBeInTheDocument();
  });

  it("requires a model under the template defined here, and flags it on that field", () => {
    renderAt("/agents/new");
    type("agent-form-name", "helper");
    fireEvent.click(screen.getByText("Define here"));
    expect(screen.getByTestId("agent-form-submit")).toHaveAttribute("aria-disabled", "true");

    const model = within(screen.getByTestId("template-form-model")).getByRole("combobox");
    fireEvent.blur(model);
    expect(model).toHaveAttribute("aria-invalid", "true");
    expect(model).toHaveAccessibleDescription(/Choose a model configuration\.$/);
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("creates the agent, shows the toast and lands on the list", async () => {
    mocks.create.mockImplementation(async (input: { name: string }) => ({ name: input.name }));
    renderAt("/agents/new");
    type("agent-form-name", "helper");
    choose("agent-form-template-ref", "shared-brain");
    fireEvent.click(screen.getByTestId("agent-form-submit"));

    expect(await screen.findByText("Agent list")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith({
      namespace: "kagent",
      name: "helper",
      resource: {
        metadata: { namespace: "kagent", name: "helper" },
        spec: { template: { ref: { name: "shared-brain" } }, harnessRef: { name: "kagent-default" } },
      },
    });
    expect(mocks.success).toHaveBeenCalledWith("Created agent helper");
  });

  it("accepts a dotted name, as the API does, and refuses capitals", async () => {
    mocks.create.mockImplementation(async (input: { name: string }) => ({ name: input.name }));
    renderAt("/agents/new");
    choose("agent-form-template-ref", "shared-brain");
    type("agent-form-name", "Team.Helper");
    expect(screen.getByText(/lowercase letters, numbers, hyphens and dots/)).toBeInTheDocument();
    expect(screen.getByTestId("agent-form-submit")).toHaveAttribute("aria-disabled", "true");

    type("agent-form-name", "team.helper");
    fireEvent.click(screen.getByTestId("agent-form-submit"));
    expect(await screen.findByText("Agent list")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: "team.helper" }));
  });

  it("explains a failed create above the buttons and keeps the form", async () => {
    mocks.create.mockRejectedValue(new Error("boom"));
    renderAt("/agents/new");
    type("agent-form-name", "helper");
    choose("agent-form-template-ref", "shared-brain");
    fireEvent.click(screen.getByTestId("agent-form-submit"));
    expect(await screen.findByText("boom")).toBeInTheDocument();
    expect(screen.getByTestId("agent-form-error")).toHaveTextContent("The agent was not created");
    expect(mocks.success).not.toHaveBeenCalled();
  });

  it("leaves a clean form without asking", async () => {
    renderAt("/agents/new");
    fireEvent.click(screen.getByTestId("agent-form-cancel"));
    expect(await screen.findByText("Agent list")).toBeInTheDocument();
  });

  it("asks before a dirty form is left, and Discard leaves", async () => {
    const router = renderAt("/agents/new");
    type("agent-form-name", "helper");
    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("agent-form-discard-body")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/agents/new");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(await screen.findByText("Agent list")).toBeInTheDocument();
  });
});

describe("agent edit form", () => {
  it("keeps the name fixed, saves the change and shows the toast", async () => {
    mocks.update.mockImplementation(async (input: { name: string }) => ({ name: input.name }));
    renderAt("/agents/kagent/helper/edit");
    expect(screen.getByTestId("agent-form-name")).toBeDisabled();
    choose("agent-form-harness-ref", "fast-lane");
    fireEvent.click(screen.getByTestId("agent-form-submit"));

    expect(await screen.findByText("Agent list")).toBeInTheDocument();
    expect(mocks.update).toHaveBeenCalledWith({
      namespace: "kagent",
      name: "helper",
      resource: {
        metadata: { namespace: "kagent", name: "helper", resourceVersion: "3" },
        spec: { template: { ref: { name: "shared-brain" } }, harnessRef: { name: "fast-lane" } },
      },
    });
    expect(mocks.success).toHaveBeenCalledWith("Saved agent helper");
  });
});
