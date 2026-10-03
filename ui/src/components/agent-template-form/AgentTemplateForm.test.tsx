import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ModelConfig } from "@/api/domain/models";
import { themeFor } from "@/theme/theme";
import { AgentTemplateForm } from "./AgentTemplateForm";
import { emptyDraft } from "./agentTemplateDraft";

const apiMocks = vi.hoisted(() => {
  const resource = (data: unknown[] | undefined, error?: Error) => ({
    data,
    isLoading: false,
    isValidating: false,
    error,
    isEmpty: Array.isArray(data) && data.length === 0 && !error,
    refresh: async () => {},
  });

  return {
    resource,
    models: resource([]),
    empty: resource([]),
  };
});

vi.mock("@/api", () => ({
  useModels: () => apiMocks.models,
  useMcpServers: () => apiMocks.empty,
  useTools: () => apiMocks.empty,
}));

function model(ref: string): ModelConfig {
  return { ref, spec: { provider: "OpenAI", model: "gpt-4o" } };
}

function renderForm({
  namespace = "team-a",
  isCreate = true,
}: {
  namespace?: string;
  isCreate?: boolean;
} = {}) {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <AgentTemplateForm
        draft={emptyDraft(namespace)}
        onChange={() => {}}
        isCreate={isCreate}
        namespace={namespace}
      />
    </ThemeProvider>,
  );
}

describe("AgentTemplateForm model configuration hints", () => {
  beforeEach(() => {
    apiMocks.models = apiMocks.resource([]);
    apiMocks.empty = apiMocks.resource([]);
  });

  it("explains an empty namespace on the create form", () => {
    apiMocks.models = apiMocks.resource([model("other/default")]);

    renderForm();

    expect(
      screen.getByText(
        /No model configurations are available in this namespace, or you do not have permission to read them\./,
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/Choose another namespace/)).toBeInTheDocument();
  });

  it("does not suggest changing namespace on the edit form", () => {
    renderForm({ isCreate: false });

    expect(
      screen.getByText(
        /No model configurations are available in this template's namespace, or you do not have permission to read them\./,
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Choose another namespace/)).toBeNull();
  });

  it("reports a load failure only when nothing can be chosen", () => {
    apiMocks.models = apiMocks.resource(undefined, new Error("boom"));

    renderForm();

    expect(
      screen.getByText(
        "Model configurations could not be loaded, so there is nothing to choose from.",
      ),
    ).toBeInTheDocument();
  });

  it("keeps the normal hint when stale choices remain after a load failure", () => {
    apiMocks.models = apiMocks.resource(
      [model("team-a/default")],
      new Error("boom"),
    );

    renderForm();

    expect(
      screen.getByText(
        "A ModelConfig in this template's own namespace. Every harness needs one except bring-your-own (BYO).",
      ),
    ).toBeInTheDocument();
  });
});
