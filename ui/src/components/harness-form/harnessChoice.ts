import type { ModelConfigSpec } from "@/api/domain/models";

/**
 * The harness a new agent starts on, when the choice is obvious: the only one in the
 * namespace, else the chart's `kagent-default`, else the only `*-default` there is.
 */
export function preferredHarness(names: string[]): string | undefined {
  if (names.length === 1) return names[0];
  if (names.includes("kagent-default")) return "kagent-default";
  const defaults = names.filter((name) => name.endsWith("-default"));
  return defaults.length === 1 ? defaults[0] : undefined;
}

/**
 * Why a harness adapter cannot run a model, when it clearly cannot; otherwise undefined.
 *
 * The rules are the harness compilers' (`go/core/internal/translator/{codex,claude}`),
 * reduced to the ones a provider and its options decide. The compiler stays the
 * authority: this only warns early, so it never blocks a save, and anything it cannot
 * judge from the spec alone is left for the compiler to report.
 */
export function harnessModelConflict(adapter: string | undefined, model: ModelConfigSpec | undefined): string | undefined {
  if (!adapter || !model) return undefined;
  const provider = model.provider;
  if (adapter === "codex") {
    if (provider === "OpenAI") {
      return model.openAI?.apiFormat === "responses"
        ? undefined
        : "Codex needs this OpenAI model configuration's API format set to responses.";
    }
    if (provider === "Bedrock") {
      return model.model.startsWith("gpt-") ? undefined : "Codex on Bedrock runs only OpenAI gpt-* models.";
    }
    return `Codex runs OpenAI (responses API) or Bedrock models, not ${provider}.`;
  }
  if (adapter === "claude") {
    return ["Anthropic", "Bedrock", "AnthropicVertexAI"].includes(provider)
      ? undefined
      : `Claude runs Anthropic, Bedrock or AnthropicVertexAI models, not ${provider}.`;
  }
  return undefined;
}
