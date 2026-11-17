import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { ModelEditPage } from "./ModelEditPage";
import { ModelNewPage } from "./ModelNewPage";

const mocks = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), success: vi.fn() }));

const stored = {
  ref: "kagent/default-model-config",
  spec: {
    model: "gpt-4.1",
    provider: "OpenAI",
    apiKeySecret: "kagent-openai",
    apiKeySecretKey: "OPENAI_API_KEY",
  },
};

vi.mock("react-hot-toast", () => ({ default: { success: mocks.success } }));
vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { models: { create: mocks.create, update: mocks.update } },
  useInvalidateModels: () => () => Promise.resolve(),
  useNamespaces: () => ({ data: [{ name: "kagent" }, { name: "team-a" }], isLoading: false, error: undefined }),
  useProviders: () => ({
    data: [{ type: "OpenAI", requiredParams: [], optionalParams: ["maxTokens"] }],
    isLoading: false,
    error: undefined,
    refresh: vi.fn(),
  }),
  useProviderModels: () => ({
    data: { OpenAI: [{ name: "gpt-4o" }] },
    isLoading: false,
    error: undefined,
    refresh: vi.fn(),
  }),
  useModel: () => ({ data: stored, isLoading: false, error: undefined, refresh: () => Promise.resolve() }),
  useModels: () => ({ refresh: () => Promise.resolve() }),
}));

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      { path: "/models", element: <p>Model list</p> },
      { path: "/models/new", element: <ModelNewPage /> },
      { path: "/models/:namespace/:name/edit", element: <ModelEditPage /> },
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

function chooseProvider() {
  fireEvent.mouseDown(within(screen.getByTestId("model-provider")).getByRole("combobox"));
  fireEvent.click(screen.getByTitle("OpenAI"));
}

// The auto-complete and password fields carry their test id on a wrapper around the input.
const type = (testId: string, value: string) => {
  const target = screen.getByTestId(testId);
  fireEvent.change(target.matches("input,textarea") ? target : target.querySelector("input")!, { target: { value } });
};

beforeEach(() => vi.clearAllMocks());

describe("model create form", () => {
  it("shows a header back link and a namespace dropdown", () => {
    renderAt("/models/new");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Models");
    expect(screen.getByTestId("model-namespace")).toHaveClass("ant-select");
  });

  it("marks every field required or optional", () => {
    renderAt("/models/new");
    chooseProvider();
    // Provider, provider model, name, namespace, authentication, API key.
    expect(screen.getAllByTestId("field-required")).toHaveLength(6);

    fireEvent.click(screen.getByText("Existing secret"));
    // The secret replaces the API key as the required credential; its key is optional.
    expect(screen.getAllByTestId("field-required")).toHaveLength(6);
    expect(screen.getAllByTestId("field-optional")).toHaveLength(1);
    expect(screen.getByText("Key in secret")).toBeInTheDocument();
  });

  it("flags every missing field on submit and sends nothing", () => {
    renderAt("/models/new");
    fireEvent.click(screen.getByTestId("model-submit"));
    expect(screen.getByText("Choose a provider.")).toBeInTheDocument();
    expect(screen.getByText("Choose a model.")).toBeInTheDocument();
    expect(screen.getByText("A name is required.")).toBeInTheDocument();
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("names the secret when the credential is an existing one", () => {
    renderAt("/models/new");
    chooseProvider();
    type("model-model", "gpt-4o");
    fireEvent.click(screen.getByText("Existing secret"));
    fireEvent.click(screen.getByTestId("model-submit"));
    expect(screen.getByText("Enter the name of the secret.")).toBeInTheDocument();
  });

  it("opens the optional parameters on the first submit when one is invalid", () => {
    renderAt("/models/new");
    chooseProvider();
    type("model-model", "gpt-4o");
    type("model-api-key", "sk-test");
    const optional = screen.getByTestId("model-optional-params");
    fireEvent.click(within(optional).getByText(/Optional parameters/));
    type("model-param-maxTokens", "lots");
    fireEvent.click(within(optional).getByText(/Optional parameters/));
    expect(within(optional).getByRole("button", { expanded: false })).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("model-submit"));
    expect(within(optional).getByRole("button", { expanded: true })).toBeInTheDocument();
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("explains a failed create above the buttons", async () => {
    mocks.create.mockRejectedValue(new Error("boom"));
    renderAt("/models/new");
    chooseProvider();
    type("model-model", "gpt-4o");
    type("model-api-key", "sk-test");
    fireEvent.click(screen.getByTestId("model-submit"));
    expect(await screen.findByTestId("model-submit-error")).toHaveTextContent(
      "The model configuration was not created",
    );
    expect(mocks.success).not.toHaveBeenCalled();
  });

  it("creates the model, shows the toast and lands on the list", async () => {
    mocks.create.mockResolvedValue(undefined);
    renderAt("/models/new");
    chooseProvider();
    type("model-model", "gpt-4o");
    type("model-api-key", "sk-test");
    fireEvent.click(screen.getByTestId("model-submit"));
    expect(await screen.findByText("Model list")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith(
      expect.objectContaining({ ref: "kagent/gpt-4o", apiKey: "sk-test" }),
    );
    expect(mocks.success).toHaveBeenCalledWith("Created model configuration gpt-4o");
  });

  it("guards the form while the create is pending, then lands on the list", async () => {
    let finish!: (value?: unknown) => void;
    mocks.create.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    const router = renderAt("/models/new");
    chooseProvider();
    type("model-model", "gpt-4o");
    type("model-api-key", "sk-test");
    fireEvent.click(screen.getByTestId("model-submit"));
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("model-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/models/new");

    finish();
    expect(await screen.findByText("Model list")).toBeInTheDocument();
  });

  it("leaves a clean form without asking", async () => {
    renderAt("/models/new");
    fireEvent.click(screen.getByTestId("model-cancel"));
    expect(await screen.findByText("Model list")).toBeInTheDocument();
  });

  it("asks before a dirty form is left, and Keep editing stays", async () => {
    const router = renderAt("/models/new");
    chooseProvider();
    fireEvent.click(screen.getByTestId("model-cancel"));
    expect(await screen.findByTestId("model-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/models/new");
  });
});

describe("model edit form", () => {
  it("locks the identity, saves and shows the toast", async () => {
    mocks.update.mockResolvedValue(undefined);
    renderAt("/models/kagent/default-model-config/edit");
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("Models");
    expect(screen.getByTestId("model-name")).toBeDisabled();
    expect(within(screen.getByTestId("model-namespace")).getByRole("combobox")).toBeDisabled();

    type("model-api-key-secret", "kagent-other");
    fireEvent.click(screen.getByTestId("model-submit"));
    expect(await screen.findByText("Model list")).toBeInTheDocument();
    expect(mocks.update).toHaveBeenCalledWith(
      "kagent",
      "default-model-config",
      expect.objectContaining({ ref: "kagent/default-model-config" }),
    );
    expect(mocks.success).toHaveBeenCalledWith("Saved model configuration default-model-config");
  });

  it("asks before a changed form is left", async () => {
    renderAt("/models/kagent/default-model-config/edit");
    type("model-api-key-secret", "kagent-other");
    fireEvent.click(screen.getByTestId("model-cancel"));
    expect(await screen.findByTestId("model-discard-body")).toBeInTheDocument();
  });
});
