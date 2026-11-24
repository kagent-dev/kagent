import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ApiResource, CreateModelConfigRequest, Provider, ProviderModelsResponse } from "@/api";
import { ApiError } from "@/api/ApiError";
import { themeFor } from "@/theme/theme";
import { emptyModelDraft } from "./modelDraft";
import { ModelForm } from "./ModelForm";

const { useProviders, useProviderModels, useNamespaces, useDiscoveredModels } = vi.hoisted(() => ({
  useProviders: vi.fn(),
  useProviderModels: vi.fn(),
  useNamespaces: vi.fn(),
  useDiscoveredModels: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useProviders,
  useProviderModels,
  useNamespaces,
  useDiscoveredModels,
}));

function resource<T>(data: T | undefined, error?: ApiError): ApiResource<T> {
  return {
    data,
    isLoading: false,
    isValidating: false,
    error,
    isEmpty: false,
    refresh: async () => {},
  };
}

const ENDPOINT = "https://litellm.example/v1";

const stockOpenAI: Provider = {
  name: "OpenAI",
  type: "OpenAI",
  requiredParams: [],
  optionalParams: ["baseUrl", "organization"],
  source: "stock",
};
const proxy: Provider = {
  name: "litellm",
  type: "OpenAI",
  requiredParams: [],
  optionalParams: [],
  source: "configured",
  endpoint: ENDPOINT,
};
const catalogue: ProviderModelsResponse = {
  OpenAI: [{ name: "gpt-4.1", function_calling: false }],
};

let discovered: ApiResource<string[]>;

beforeEach(() => {
  discovered = resource(["team-a/sonnet", "team-a/llama"]);
  useProviders.mockReturnValue(resource([stockOpenAI, proxy]));
  useProviderModels.mockReturnValue(resource(catalogue));
  useNamespaces.mockReturnValue(resource([{ name: "kagent", status: "Active" }]));
  useDiscoveredModels.mockImplementation((name?: string) =>
    name ? discovered : resource(undefined),
  );
});

function renderForm(
  onSubmit = vi.fn<(payload: CreateModelConfigRequest) => Promise<void>>(async () => {}),
  params: Record<string, string> = {}) {
  render(
    <ThemeProvider theme={themeFor("light")}>
      <MemoryRouter>
        <ModelForm
          onSubmit={onSubmit}
          initial={{
            ...emptyModelDraft(),
            name: "m",
            provider: "OpenAI",
            model: "gpt-4.1",
            apiKey: "k",
            params,
          }}
        />
      </MemoryRouter>
    </ThemeProvider>,
  );
  return onSubmit;
}

const baseUrl = () => screen.getByTestId("model-param-baseUrl") as HTMLInputElement;
const modelBox = () => within(screen.getByTestId("model-model")).getByRole("combobox");

/** Opens the optional-parameters panel, where the base URL lives. */
async function openOptionalParams() {
  await userEvent.click(screen.getByText(/Optional parameters/));
}

async function pickSource() {
  await userEvent.click(screen.getByRole("combobox", { name: "Discover models from" }));
  await userEvent.click(await screen.findByText(ENDPOINT, { selector: ".ant-typography" }));
}

async function expectModelOptions(expected: string[]) {
  // The picker filters its options by the text in it, so it is emptied first.
  await userEvent.clear(modelBox());
  await userEvent.click(modelBox());
  // Popups another select is still closing count as open under jsdom, so the
  // model picker's is found by its options rather than by being the only one.
  await waitFor(() => {
    const popups = Array.from(
      document.querySelectorAll(".ant-select-dropdown:not(.ant-select-dropdown-hidden)"),
    ).map((popup) =>
      Array.from(popup.querySelectorAll(".ant-select-item-option-content")).map(
        (node) => node.textContent,
      ),
    );
    expect(popups).toContainEqual(expected);
  });
  await userEvent.click(document.body);
}

describe("ModelForm model discovery", () => {
  it("adds no control when the provider type has no configured source", () => {
    useProviders.mockReturnValue(resource([stockOpenAI]));
    renderForm();

    expect(screen.queryByTestId("model-discovery-source")).not.toBeInTheDocument();
  });

  it("keeps OpenAI's parameters when a configured source shares its type", async () => {
    // Listed first, so a lookup by type alone would find it before the stock entry.
    useProviders.mockReturnValue(resource([proxy, stockOpenAI]));
    renderForm();
    await openOptionalParams();

    expect(screen.getByTestId("model-param-organization")).toBeInTheDocument();
    expect(baseUrl()).toBeInTheDocument();
  });

  it("offers the discovered models and fills the base URL when a source is chosen", async () => {
    renderForm();
    await openOptionalParams();
    await expectModelOptions(["gpt-4.1"]);

    await pickSource();

    expect(baseUrl().value).toBe(ENDPOINT);
    await expectModelOptions(["team-a/sonnet", "team-a/llama"]);
  });

  it("does not warn about function calling for a discovered model", async () => {
    renderForm();
    expect(screen.getByTestId("model-fc-warning")).toBeInTheDocument();

    await pickSource();

    expect(screen.queryByTestId("model-fc-warning")).not.toBeInTheDocument();
  });

  it("keeps a base URL the user typed", async () => {
    renderForm(undefined, { baseUrl: "https://mine.example/v1" });
    await openOptionalParams();

    await pickSource();

    expect(baseUrl().value).toBe("https://mine.example/v1");
  });

  it("restores the built-in list and clears an auto-filled base URL when the source is cleared", async () => {
    renderForm();
    await openOptionalParams();
    await pickSource();

    const select = screen.getByTestId("model-discovery-source");
    await userEvent.hover(select);
    await userEvent.click(select.querySelector(".ant-select-clear")!);
    await userEvent.keyboard("{Escape}");

    await waitFor(() => expect(baseUrl().value).toBe(""));
    await expectModelOptions(["gpt-4.1"]);
  });

  it("keeps a typed base URL when the source is cleared", async () => {
    renderForm(undefined, { baseUrl: "https://mine.example/v1" });
    await openOptionalParams();
    await pickSource();

    const select = screen.getByTestId("model-discovery-source");
    await userEvent.hover(select);
    await userEvent.click(select.querySelector(".ant-select-clear")!);
    await userEvent.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByTestId("model-discovery-error")).toBeNull());
    expect(baseUrl().value).toBe("https://mine.example/v1");
  });

  it("shows the failure, offers a retry, and still takes a typed model id", async () => {
    const refresh = vi.fn(async () => {});
    discovered = {
      ...resource<string[]>(undefined, new ApiError("endpoint down", { kind: "network", url: "ModelService/ListProviderModels" })),
      refresh,
    };
    const onSubmit = renderForm();

    await pickSource();

    expect(await screen.findByTestId("model-discovery-error")).toBeInTheDocument();
    await userEvent.click(within(screen.getByTestId("model-discovery-error")).getByRole("button"));
    expect(refresh).toHaveBeenCalled();

    await userEvent.clear(modelBox());
    await userEvent.type(modelBox(), "typed-id");
    await userEvent.click(screen.getByTestId("model-submit"));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    expect(onSubmit.mock.calls[0][0].spec.model).toBe("typed-id");
  });

  it("does not put the source in the submitted payload", async () => {
    const onSubmit = renderForm();
    await pickSource();

    await userEvent.click(screen.getByTestId("model-submit"));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    // The endpoint lands in the OpenAI block as the base URL, like any typed one;
    // the source's own name appears nowhere.
    expect(onSubmit.mock.calls[0][0]).toEqual({
      ref: "kagent/m",
      apiKey: "k",
      spec: {
        model: "gpt-4.1",
        provider: "OpenAI",
        openAI: { baseUrl: ENDPOINT },
      },
    });
  });
});
