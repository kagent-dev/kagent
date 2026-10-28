import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "@/api/ApiError";
import { themeFor } from "@/theme/theme";
import { ModelForm } from "./ModelForm";

const hooks = vi.hoisted(() => ({ sources: vi.fn(), discovery: vi.fn() }));
vi.mock("@/api", async importOriginal => ({
  ...await importOriginal<typeof import("@/api")>(),
  useConfiguredModelProviders: hooks.sources,
  useModelDiscovery: hooks.discovery,
  useNamespaces: () => ({ data: ["kagent"] }),
  useProviders: () => ({ data: [{ name: "OpenAI", type: "OpenAI", requiredParams: [], optionalParams: ["baseUrl"] }] }),
  useProviderModels: () => ({ data: {} }),
}));

afterEach(() => { cleanup(); vi.clearAllMocks(); });

async function select(testId: string, label: string) {
  fireEvent.mouseDown(within(screen.getByTestId(testId)).getByRole("combobox"));
  fireEvent.click(await screen.findByText(label, { selector: ".ant-select-item-option-content" }));
}

it.each(["deleted", "forbidden", "network", "removed from list"])("clears and blocks cached selection when the source is %s", async failure => {
  const catalog = { endpoint: "https://gateway/v1", state: "current", models: [{ id: "model-a" }] };
  hooks.sources.mockReturnValue({ data: [{ namespace: "kagent", name: "gateway" }] });
  hooks.discovery.mockReturnValue({ data: catalog });
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  const form = <ThemeProvider theme={themeFor("dark")}><MemoryRouter><ModelForm onSubmit={onSubmit} /></MemoryRouter></ThemeProvider>;
  const { rerender } = render(form);
  await select("model-discovery-source", "kagent/gateway");
  await select("model-invocation-format", "Chat Completions");
  const model = within(screen.getByTestId("model-model")).getByRole("combobox");
  fireEvent.change(model, { target: { value: "model-a" } });
  fireEvent.change(screen.getByTestId("model-name"), { target: { value: "my-model" } });
  fireEvent.click(within(screen.getByTestId("model-auth-type")).getByText("No credential", { exact: true }));
  expect(model).toHaveValue("model-a");
  if (failure === "removed from list") {
    hooks.sources.mockReturnValue({ data: [] });
  } else {
    const status = failure === "deleted" ? 404 : failure === "forbidden" ? 403 : 503;
    hooks.discovery.mockReturnValue({ data: catalog, error: new ApiError("Source unavailable", { kind: "http", status, url: "discovery" }) });
  }
  rerender(<ThemeProvider theme={themeFor("dark")}><MemoryRouter><ModelForm onSubmit={onSubmit} /></MemoryRouter></ThemeProvider>);
  expect(model).toHaveValue("");
  expect(model).toBeDisabled();
  fireEvent.click(screen.getByTestId("model-submit"));
  expect(onSubmit).not.toHaveBeenCalled();

  hooks.sources.mockReturnValue({ data: [{ namespace: "kagent", name: "gateway" }] });
  hooks.discovery.mockReturnValue({ data: catalog });
  rerender(<ThemeProvider theme={themeFor("dark")}><MemoryRouter><ModelForm onSubmit={onSubmit} /></MemoryRouter></ThemeProvider>);
  expect(model).toBeEnabled();
  expect(model).toHaveValue("");
  fireEvent.change(model, { target: { value: "model-a" } });
  fireEvent.click(screen.getByTestId("model-submit"));
  expect(onSubmit).toHaveBeenCalledOnce();
});
