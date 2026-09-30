import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useModels, type ApiResource, type ModelConfig } from "@/api";
import { ApiError } from "@/api/ApiError";
import { themeFor } from "@/theme/theme";
import { AgentTemplateForm } from "./AgentTemplateForm";
import { emptyDraft } from "./agentTemplateDraft";

vi.mock("@/api", () => ({
  useModels: vi.fn(),
  useMcpServers: () => ({ data: [] }),
  useTools: () => ({ data: [] }),
}));

const model: ModelConfig = {
  ref: "kagent/default-model",
  spec: { model: "test-model", provider: "OpenAI" },
};
const loadError = new ApiError("Unavailable", { kind: "network", url: "models.list" });

function models(overrides: Partial<ApiResource<ModelConfig[]>> = {}) {
  vi.mocked(useModels).mockReturnValue({
    data: [], isLoading: false, isValidating: false, error: undefined,
    isEmpty: true, refresh: async () => {}, ...overrides,
  });
}

function renderForm(props: { isCreate?: boolean; readOnly?: boolean; namespace?: string } = {}) {
  const namespace = props.namespace ?? "kagent";
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <AgentTemplateForm
        draft={{ ...emptyDraft(namespace), name: "test-template" }}
        onChange={vi.fn()}
        isCreate
        namespace={namespace}
        {...props}
      />
    </ThemeProvider>,
  );
}

describe("agent template model selection", () => {
  beforeEach(() => models());

  it("explains an empty namespace without assuming models are visible to this user", () => {
    models({ data: [model], isEmpty: false });
    renderForm({ namespace: "other" });

    expect(screen.getByText(/No model configurations are available in namespace "other"/)).toBeVisible();
    expect(screen.getByText(/Choose another namespace or ask an administrator/)).toBeVisible();
    expect(screen.getByText(/add a model configuration or check your access/)).toBeVisible();
    expect(screen.queryByText(/A model configuration is required/)).toBeNull();
  });

  it("does not suggest changing the namespace of an existing template", () => {
    renderForm({ isCreate: false });

    expect(screen.getByText(/No model configurations are available in namespace "kagent"/)).toBeVisible();
    expect(screen.getByText(/Ask an administrator to add a model configuration or check your access/)).toBeVisible();
    expect(screen.queryByText(/Choose another namespace/)).toBeNull();
  });

  it("reports a failed load instead of describing it as an empty namespace", () => {
    models({ data: undefined, error: loadError });
    renderForm();

    expect(screen.getByText(/Could not load model configurations/)).toBeVisible();
    expect(screen.queryByText(/No model configurations are available/)).toBeNull();
  });

  it("reports a failed refresh when cached models belong to another namespace", () => {
    models({ data: [model], error: loadError, isEmpty: false });
    renderForm({ namespace: "other" });

    expect(screen.getByText(/Could not load model configurations/)).toBeVisible();
  });

  it("keeps the normal hint when a failed refresh leaves usable cached models", () => {
    models({ data: [model], error: loadError, isEmpty: false });
    renderForm();

    expect(screen.getByText(/Every harness needs one except bring-your-own/)).toBeVisible();
    expect(screen.queryByText(/Could not load model configurations/)).toBeNull();
    expect(screen.queryByText(/No model configurations are available/)).toBeNull();
  });

  it("does not claim the namespace is empty during the first load", () => {
    models({ data: undefined, isLoading: true, isValidating: true, isEmpty: false });
    renderForm();

    expect(screen.queryByText(/No model configurations are available/)).toBeNull();
    expect(screen.queryByText(/Could not load model configurations/)).toBeNull();
  });

  it("keeps read-only details free of model selection guidance", () => {
    models({ data: undefined, error: loadError });
    renderForm({ isCreate: false, readOnly: true });

    expect(screen.queryByText(/Could not load model configurations/)).toBeNull();
    expect(screen.queryByText(/No model configurations are available/)).toBeNull();
  });
});
