import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { emptyDraft } from "./agentTemplateDraft";
import { AgentTemplateForm } from "./AgentTemplateForm";

const { useModels, useMcpServers, useTools } = vi.hoisted(() => ({
  useModels: vi.fn(),
  useMcpServers: vi.fn(),
  useTools: vi.fn(),
}));

vi.mock("@/api", () => ({ useModels, useMcpServers, useTools }));

function apiResource(data: unknown, error?: Error) {
  return {
    data,
    error,
    isLoading: false,
    isValidating: false,
    isEmpty: Array.isArray(data) && data.length === 0,
    refresh: vi.fn(),
  };
}

function renderForm({ isCreate = true }: { isCreate?: boolean } = {}) {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <AgentTemplateForm
        draft={emptyDraft("team-a")}
        onChange={vi.fn()}
        isCreate={isCreate}
        namespace="team-a"
      />
    </ThemeProvider>,
  );
}

beforeEach(() => {
  useModels.mockReturnValue(apiResource([]));
  useMcpServers.mockReturnValue(apiResource([]));
  useTools.mockReturnValue(apiResource([]));
});

describe("AgentTemplateForm model configuration guidance", () => {
  it("explains an empty namespace on create", () => {
    renderForm();

    expect(
      screen.getByText(
        "No model configurations are available in this namespace. Choose another namespace or ask an administrator to add one.",
      ),
    ).toBeInTheDocument();
  });

  it("does not suggest changing namespace on edit", () => {
    renderForm({ isCreate: false });

    expect(
      screen.getByText(
        "No model configurations are available in this namespace. Ask an administrator to add one.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Choose another namespace/)).toBeNull();
  });

  it("reports a failed model read when it leaves no choices", () => {
    useModels.mockReturnValue(apiResource(undefined, new Error("unavailable")));

    renderForm();

    expect(
      screen.getByText("Model configurations could not be loaded."),
    ).toBeInTheDocument();
  });

  it("keeps the ordinary guidance when stale choices remain after an error", () => {
    useModels.mockReturnValue(
      apiResource(
        [{ ref: "team-a/gpt", spec: { provider: "OpenAI", model: "gpt-5" } }],
        new Error("background refresh failed"),
      ),
    );

    renderForm();

    expect(
      screen.getByText(
        "A ModelConfig in this template's own namespace. Every harness needs one except bring-your-own (BYO).",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Model configurations could not be loaded.")).toBeNull();
  });
});
