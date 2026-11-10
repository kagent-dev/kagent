import { describe, expect, it } from "vitest";
import { harnessModelConflict, preferredHarness } from "./harnessChoice";
import type { ModelConfigSpec } from "@/api/domain/models";

describe("the harness a new agent starts on", () => {
  it.each([
    [[], undefined],
    [["mine"], "mine"],
    [["codex-default", "kagent-default", "mine"], "kagent-default"],
    [["claude-default", "mine"], "claude-default"],
    [["claude-default", "codex-default"], undefined],
    [["a", "b"], undefined],
  ] as [string[], string | undefined][])("from %o is %s", (names, preferred) => {
    expect(preferredHarness(names)).toBe(preferred);
  });
});

const openai = (apiFormat?: "chatCompletions" | "responses"): ModelConfigSpec => ({
  provider: "OpenAI",
  model: "gpt-5",
  ...(apiFormat ? { openAI: { apiFormat } } : {}),
});

describe("harness and model compatibility", () => {
  it.each([
    ["codex", openai("responses"), undefined],
    ["codex", openai("chatCompletions"), "Codex needs this OpenAI model configuration's API format set to responses."],
    ["codex", openai(), "Codex needs this OpenAI model configuration's API format set to responses."],
    ["codex", { provider: "Bedrock", model: "gpt-oss" }, undefined],
    ["codex", { provider: "Bedrock", model: "anthropic.claude" }, "Codex on Bedrock runs only OpenAI gpt-* models."],
    ["codex", { provider: "Anthropic", model: "claude" }, "Codex runs OpenAI (responses API) or Bedrock models, not Anthropic."],
    ["claude", { provider: "Anthropic", model: "claude" }, undefined],
    ["claude", { provider: "AnthropicVertexAI", model: "claude" }, undefined],
    ["claude", openai("responses"), "Claude runs Anthropic, Bedrock or AnthropicVertexAI models, not OpenAI."],
    ["kagent", openai(), undefined],
    ["byo", { provider: "Ollama", model: "llama" }, undefined],
  ] as [string, ModelConfigSpec, string | undefined][])("%s with %o", (adapter, model, conflict) => {
    expect(harnessModelConflict(adapter, model)).toBe(conflict);
  });

  it("says nothing until both the adapter and the model are known", () => {
    expect(harnessModelConflict(undefined, openai())).toBeUndefined();
    expect(harnessModelConflict("codex", undefined)).toBeUndefined();
  });
});
