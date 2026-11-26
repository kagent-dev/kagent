import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { PromptEditPage } from "./PromptEditPage";
import { PromptNewPage } from "./PromptNewPage";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  success: vi.fn(),
  prompt: { namespace: "kagent", name: "team-prompts", data: { tone: "Be concise." } },
}));

vi.mock("react-hot-toast", () => ({ default: { success: mocks.success } }));
vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { prompts: { create: mocks.create, update: mocks.update } },
  useInvalidatePrompts: () => () => Promise.resolve(),
  useNamespaces: () => ({ data: [{ name: "kagent" }, { name: "team-a" }], isLoading: false, error: undefined }),
  usePrompt: () => ({ data: mocks.prompt, isLoading: false, error: undefined, refresh: vi.fn() }),
}));

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      { path: "/prompts", element: <p>Library list</p> },
      { path: "/prompts/new", element: <PromptNewPage /> },
      { path: "/prompts/:namespace/:name", element: <p>Library detail</p> },
      { path: "/prompts/:namespace/:name/edit", element: <PromptEditPage /> },
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

beforeEach(() => vi.clearAllMocks());

describe("prompt library create form", () => {
  it("shows a header back link and marks every field required or optional", () => {
    renderAt("/prompts/new");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Prompts");
    // Name, Namespace, Fragments and the first fragment's key.
    expect(screen.getAllByTestId("field-required")).toHaveLength(4);
    expect(screen.getAllByTestId("field-optional")).toHaveLength(1);
  });

  it("picks the namespace from a dropdown", () => {
    renderAt("/prompts/new");
    expect(screen.getByTestId("prompt-namespace")).toHaveClass("ant-select");
  });

  it("holds Create back until the required fields are filled, and flags a field once left", () => {
    renderAt("/prompts/new");
    const submit = screen.getByTestId("prompt-submit");
    expect(submit).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(submit);
    expect(mocks.create).not.toHaveBeenCalled();
    expect(screen.queryByText("A library name is required.")).not.toBeInTheDocument();

    fireEvent.blur(screen.getByTestId("prompt-name"));
    expect(screen.getByText("A library name is required.")).toBeInTheDocument();
    expect(screen.queryByText("Add at least one fragment key.")).not.toBeInTheDocument();

    type("prompt-name", "team-prompts");
    type("fragment-key", "tone");
    expect(submit).not.toHaveAttribute("aria-disabled");
  });

  it("explains a failed save above the buttons and keeps the form", async () => {
    mocks.create.mockRejectedValue(new Error("boom"));
    renderAt("/prompts/new");
    type("prompt-name", "team-prompts");
    type("fragment-key", "tone");
    fireEvent.click(screen.getByTestId("prompt-submit"));
    expect(await screen.findByTestId("prompt-submit-error")).toHaveTextContent("The prompt library was not created");
    expect(mocks.success).not.toHaveBeenCalled();
  });

  it("creates the library, shows the toast and lands on the list", async () => {
    mocks.create.mockResolvedValue(undefined);
    renderAt("/prompts/new");
    type("prompt-name", "team-prompts");
    type("fragment-key", "tone");
    type("fragment-value", "Be concise.");
    fireEvent.click(screen.getByTestId("prompt-submit"));
    expect(await screen.findByText("Library list")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith({
      namespace: "kagent",
      name: "team-prompts",
      data: { tone: "Be concise." },
    });
    expect(mocks.success).toHaveBeenCalledWith("Created prompt library team-prompts");
  });

  it("guards the form while the create is pending, then lands on the list", async () => {
    let finish!: (value?: unknown) => void;
    mocks.create.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    const router = renderAt("/prompts/new");
    type("prompt-name", "team-prompts");
    type("fragment-key", "tone");
    type("fragment-value", "Be concise.");
    fireEvent.click(screen.getByTestId("prompt-submit"));
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("prompt-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/prompts/new");

    finish();
    expect(await screen.findByText("Library list")).toBeInTheDocument();
  });

  it("leaves a clean form without asking", async () => {
    renderAt("/prompts/new");
    fireEvent.click(screen.getByTestId("prompt-cancel"));
    expect(await screen.findByText("Library list")).toBeInTheDocument();
  });

  it("asks before a dirty form is left, and Keep editing stays", async () => {
    const router = renderAt("/prompts/new");
    type("prompt-name", "team-prompts");
    fireEvent.click(screen.getByTestId("prompt-cancel"));
    expect(await screen.findByTestId("prompt-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/prompts/new");
    expect(screen.getByTestId("prompt-name")).toHaveValue("team-prompts");
  });
});

describe("prompt library edit form", () => {
  it("locks the identity, saves the fragments and shows the toast", async () => {
    mocks.update.mockResolvedValue(undefined);
    renderAt("/prompts/kagent/team-prompts/edit");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Prompts");
    expect(screen.getByTestId("prompt-name")).toHaveAttribute("readonly");
    expect(within(screen.getByTestId("prompt-namespace")).getByRole("combobox")).toBeDisabled();

    type("fragment-value", "Be brief.");
    fireEvent.click(screen.getByTestId("prompt-submit"));
    expect(await screen.findByText("Library detail")).toBeInTheDocument();
    expect(mocks.update).toHaveBeenCalledWith("kagent", "team-prompts", { data: { tone: "Be brief." } });
    expect(mocks.success).toHaveBeenCalledWith("Saved prompt library team-prompts");
  });

  it("asks before a changed form is left", async () => {
    renderAt("/prompts/kagent/team-prompts/edit");
    type("fragment-value", "Be brief.");
    fireEvent.click(screen.getByTestId("prompt-cancel"));
    expect(await screen.findByTestId("prompt-discard-body")).toBeInTheDocument();
  });
});
