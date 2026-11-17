import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { ConfigProvider } from "antd";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTemplate } from "@/api/domain/agentTemplates";
import { themeFor } from "@/theme/theme";
import { AgentTemplateNewPage } from "./AgentTemplateNewPage";
import { AgentTemplateDetailsPage } from "./AgentTemplateDetailsPage";
import { FIX_FIELDS_MESSAGE, FIX_FIELDS_TITLE } from "@/components/form/useSubmitValidation";

const api = vi.hoisted(() => ({
  createAgentTemplate: vi.fn(),
  updateAgentTemplate: vi.fn(),
  toastSaved: vi.fn(),
  listedNamespace: [] as (string | undefined)[],
  templatesError: undefined as Error | undefined,
  namespacesError: undefined as Error | undefined,
}));

function template(name: string, extra: Partial<AgentTemplate["resource"]["spec"]> = {}): AgentTemplate {
  return {
    ref: `kagent/${name}`,
    namespace: "kagent",
    name,
    modelConfigRef: "",
    description: "",
    resource: { metadata: { name, namespace: "kagent" }, spec: { description: "Before", ...extra } },
  };
}
const EDITED = template("triage");
const TEMPLATES = [EDITED, template("reviewer"), template("summarizer")];

const resource = <T,>(data: T) => ({
  data, error: undefined, isLoading: false, isValidating: false, isEmpty: false, refresh: () => Promise.resolve(),
});

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: {
    agentBuildingBlocks: {
      createAgentTemplate: api.createAgentTemplate,
      updateAgentTemplate: api.updateAgentTemplate,
      removeAgentTemplate: vi.fn(),
    },
  },
  useNamespaces: () =>
    api.namespacesError
      ? { ...resource(undefined), error: api.namespacesError }
      : resource([{ name: "kagent" }, { name: "default" }]),
  useModels: () => resource([]),
  useMcpServers: () => resource([]),
  useTools: () => resource([]),
  useAgentTemplates: (namespace?: string) => {
    api.listedNamespace.push(namespace);
    return api.templatesError ? { ...resource(undefined), error: api.templatesError } : resource(TEMPLATES);
  },
  useAgentTemplate: (namespace?: string, name?: string) =>
    resource(namespace && name === EDITED.name ? EDITED : undefined),
  useInvalidateAgentTemplates: () => () => Promise.resolve(),
  useAgentsAcrossNamespaces: () => resource({ agents: [] }),
  useAgentConversations: () => resource({ all: [] }),
}));
vi.mock("@/components/form/saveToast", () => ({ toastSaved: api.toastSaved }));

beforeEach(() => {
  api.createAgentTemplate.mockReset();
  api.updateAgentTemplate.mockReset();
  api.toastSaved.mockReset();
  api.listedNamespace = [];
  api.templatesError = undefined;
  api.namespacesError = undefined;
});

function renderAt(entry: string) {
  const router = createMemoryRouter(
    [
      { path: "/agent-templates/new", element: <AgentTemplateNewPage /> },
      { path: "/agent-templates/:namespace/:name", element: <AgentTemplateDetailsPage /> },
      { path: "/agents", element: <p>Agents list</p> },
    ],
    { initialEntries: [entry] },
  );
  render(
    <ThemeProvider theme={themeFor("light")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <RouterProvider router={router} />
      </ConfigProvider>
    </ThemeProvider>,
  );
  return router;
}

const fill = (testId: string, value: string) =>
  fireEvent.change(screen.getByTestId(testId), { target: { value } });

/** Opens a select by test id and picks the option with this title. */
function choose(testId: string, title: string) {
  fireEvent.mouseDown(within(screen.getByTestId(testId)).getByRole("combobox"));
  fireEvent.click(document.querySelector(`.ant-select-item-option[title="${title}"]`)!);
}

function optionTitles(testId: string): string[] {
  fireEvent.mouseDown(within(screen.getByTestId(testId)).getByRole("combobox"));
  return [...document.querySelectorAll(".ant-select-item-option")].map((option) => option.getAttribute("title") ?? "");
}

/** "required" or "optional", read off the label with this text. */
function markOn(text: string): string {
  const label = [...document.querySelectorAll(".ant-form-item-label label")].find(
    (node) => node.textContent?.replace(/\*|\(required\)|\(optional\)/g, "").trim() === text,
  );
  if (!label) throw new Error(`No label "${text}"`);
  if (label.querySelector('[data-testid="field-required"]')) return "required";
  if (label.querySelector('[data-testid="field-optional"]')) return "optional";
  return "unmarked";
}

function addSubAgent(template: string) {
  fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
  choose("template-form-sub-agent-template-0", template);
  fill("template-form-sub-agent-name-0", "review");
  fill("template-form-sub-agent-description-0", "Review a change.");
}

describe("new agent template page", () => {
  it("marks every field required or optional", () => {
    renderAt("/agent-templates/new");
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
    fireEvent.click(screen.getByTestId("template-form-labels-add"));

    for (const field of ["Name", "Namespace", "Output format", "Agent template", "Tool name", "When to use it", "Key"]) {
      expect(markOn(field), field).toBe("required");
    }
    for (const field of ["Model configuration", "Description", "System prompt", "MCP server tools", "Agent templates as tools", "Labels", "Value"]) {
      expect(markOn(field), field).toBe("optional");
    }
  });

  it("shows inline errors and a submit error on an invalid submit, and sends nothing", () => {
    renderAt("/agent-templates/new");
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));

    const submit = screen.getByTestId("template-submit");
    expect(submit).toBeEnabled();
    fireEvent.click(submit);

    expect(screen.getByText("A name is required.")).toBeInTheDocument();
    expect(screen.getByText("Choose an agent template.")).toBeInTheDocument();
    expect(screen.getByText("Enter a tool name.")).toBeInTheDocument();
    expect(screen.getByText("Say when to use this tool.")).toBeInTheDocument();
    expect(screen.getByTestId("template-create-error")).toHaveTextContent(FIX_FIELDS_TITLE);
    expect(screen.getByTestId("template-create-error")).toHaveTextContent(FIX_FIELDS_MESSAGE);
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(api.createAgentTemplate).not.toHaveBeenCalled();
  });

  it("offers the namespace's agent templates as sub-agents and sends a valid draft", async () => {
    api.createAgentTemplate.mockResolvedValue({ name: "helper" });
    const router = renderAt("/agent-templates/new");

    fill("template-form-name", "helper");
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
    expect(optionTitles("template-form-sub-agent-template-0")).toEqual(["triage", "reviewer", "summarizer"]);
    expect(api.listedNamespace).toContain("kagent");
    fireEvent.keyDown(within(screen.getByTestId("template-form-sub-agent-template-0")).getByRole("combobox"), { key: "Escape" });
    choose("template-form-sub-agent-template-0", "reviewer");
    fill("template-form-sub-agent-name-0", "review");
    fill("template-form-sub-agent-description-0", "Review a change.");
    fireEvent.click(screen.getByTestId("template-submit"));

    await waitFor(() => expect(router.state.location.pathname).toBe("/agents"));
    expect(api.createAgentTemplate).toHaveBeenCalledWith({
      namespace: "kagent",
      name: "helper",
      resource: {
        metadata: { name: "helper", namespace: "kagent", labels: {} },
        spec: { tools: [{ subAgent: { name: "review", description: "Review a change.", templateRef: { name: "reviewer" } } }] },
      },
    });
    expect(api.toastSaved).toHaveBeenCalledWith("created", "agent template", "helper");
    expect(router.state.location.search).toBe("?tab=templates&ns=kagent");
  });

  it("clears sub-agents when the namespace changes, since they name templates in the old one", () => {
    renderAt("/agent-templates/new");
    addSubAgent("reviewer");
    choose("template-form-namespace", "default");
    expect(screen.queryByTestId("template-form-sub-agent-0")).not.toBeInTheDocument();
  });

  it("takes a typed namespace exactly as typed when the list cannot be read", async () => {
    api.namespacesError = new Error("forbidden");
    api.createAgentTemplate.mockResolvedValue({ name: "helper" });
    const router = renderAt("/agent-templates/new");
    fill("template-form-name", "helper");
    const box = within(screen.getByTestId("template-form-namespace")).getByRole("textbox");

    fireEvent.change(box, { target: { value: "" } });
    expect(box).toHaveValue("");
    fireEvent.click(screen.getByTestId("template-submit"));
    expect(screen.getByText("A namespace is required.")).toBeInTheDocument();
    expect(api.createAgentTemplate).not.toHaveBeenCalled();

    fireEvent.change(box, { target: { value: "team-b" } });
    expect(box).toHaveValue("team-b");
    fireEvent.click(screen.getByTestId("template-submit"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/agents"));
    expect(api.createAgentTemplate).toHaveBeenCalledWith(expect.objectContaining({ namespace: "team-b" }));
  });

  it("lets a sub-agent template be typed when the list cannot be read", async () => {
    api.templatesError = new Error("forbidden");
    api.createAgentTemplate.mockResolvedValue({ name: "helper" });
    const router = renderAt("/agent-templates/new");

    fill("template-form-name", "helper");
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
    fill("template-form-sub-agent-template-0", "reviewer");
    fill("template-form-sub-agent-name-0", "review");
    fill("template-form-sub-agent-description-0", "Review a change.");
    fireEvent.click(screen.getByTestId("template-submit"));

    await waitFor(() => expect(router.state.location.pathname).toBe("/agents"));
    expect(api.createAgentTemplate).toHaveBeenCalledWith(expect.objectContaining({
      resource: expect.objectContaining({
        spec: { tools: [{ subAgent: { name: "review", description: "Review a change.", templateRef: { name: "reviewer" } } }] },
      }),
    }));
  });

  it("guards the form while the create is pending, then lands on the list", async () => {
    let finish!: () => void;
    api.createAgentTemplate.mockReturnValue(new Promise((resolve) => { finish = () => resolve({ name: "helper" }); }));
    const router = renderAt("/agent-templates/new");
    fill("template-form-name", "helper");
    fireEvent.click(screen.getByTestId("template-submit"));
    await waitFor(() => expect(api.createAgentTemplate).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("template-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/agent-templates/new");

    finish();
    await waitFor(() => expect(router.state.location.pathname).toBe("/agents"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("cancels and goes back to the list when nothing was entered", async () => {
    const router = renderAt("/agent-templates/new");
    expect(screen.getByTestId("form-back-link")).toHaveAttribute("href", "/agents?tab=templates");
    fireEvent.click(screen.getByTestId("template-cancel"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/agents"));
  });

  it("asks before throwing away a draft", async () => {
    const router = renderAt("/agent-templates/new");
    fill("template-form-name", "helper");

    fireEvent.click(screen.getByTestId("template-cancel"));
    expect(await screen.findByTestId("template-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/agent-templates/new");
    expect(screen.getByTestId("template-form-name")).toHaveValue("helper");

    fireEvent.click(screen.getByTestId("form-back-link"));
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/agents"));
  });
});

describe("agent template details page, edit mode", () => {
  function startEditing() {
    const router = renderAt("/agent-templates/kagent/triage");
    expect(screen.getByTestId("template-form-description")).toHaveAttribute("readonly");
    // Reading is not a form, so nothing is marked.
    expect(document.querySelector('[data-testid="field-required"], [data-testid="field-optional"]')).toBeNull();
    fireEvent.click(screen.getByTestId("template-edit"));
    return router;
  }

  it("offers every other template in the namespace, never itself", () => {
    startEditing();
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
    expect(optionTitles("template-form-sub-agent-template-0")).toEqual(["reviewer", "summarizer"]);
  });

  it("keeps ids unique across repeated rows and their errors", () => {
    startEditing();
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
    fireEvent.click(screen.getByTestId("template-submit"));
    // antd gives its own unnamed selects a fixed "test-id" under test, so those are skipped.
    const ids = [...document.body.querySelectorAll("[id]")].map((node) => node.id).filter((id) => id !== "test-id");
    expect(ids.filter((id, at) => ids.indexOf(id) !== at)).toEqual([]);
  });

  it("shows inline errors on an invalid save and sends nothing", () => {
    startEditing();
    fireEvent.click(screen.getByTestId("template-form-sub-agents-add"));
    fireEvent.click(screen.getByTestId("template-submit"));

    expect(screen.getByText("Choose an agent template.")).toBeInTheDocument();
    expect(screen.getByTestId("template-save-error")).toHaveTextContent(FIX_FIELDS_TITLE);
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
    expect(api.updateAgentTemplate).not.toHaveBeenCalled();
  });

  it("saves the edit, toasts, and returns to reading", async () => {
    api.updateAgentTemplate.mockResolvedValue({ name: "triage" });
    startEditing();
    fill("template-form-description", "After");
    addSubAgent("summarizer");
    fireEvent.click(screen.getByTestId("template-submit"));

    await waitFor(() => expect(api.toastSaved).toHaveBeenCalledWith("saved", "agent template", "triage"));
    expect(api.updateAgentTemplate.mock.calls[0][0].resource.spec).toEqual({
      description: "After",
      tools: [{ subAgent: { name: "review", description: "Review a change.", templateRef: { name: "summarizer" } } }],
    });
    expect(await screen.findByTestId("template-edit")).toBeInTheDocument();
  });

  it("asks before Cancel throws away an edit, and Discard returns to reading", async () => {
    startEditing();
    fill("template-form-description", "After");

    fireEvent.click(screen.getByTestId("template-cancel"));
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    expect(await screen.findByTestId("template-edit")).toBeInTheDocument();
    expect(screen.getByTestId("template-form-description")).toHaveValue("Before");
  });

  it("goes to the list after deleting mid-edit, without asking to discard", async () => {
    const router = startEditing();
    fill("template-form-description", "After");

    fireEvent.click(screen.getByRole("button", { name: "Delete agent template triage" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/agents"));
    expect(screen.queryByTestId("template-discard-body")).not.toBeInTheDocument();
  });

  it("asks before navigating away from an edit", async () => {
    const router = startEditing();
    fill("template-form-description", "After");

    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("template-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/agent-templates/kagent/triage");
    expect(screen.getByTestId("template-form-description")).toHaveValue("After");
  });
});
