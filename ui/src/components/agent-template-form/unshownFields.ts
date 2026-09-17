import type { AgentTemplateSpec } from "@/api/domain/agentTemplates";

/**
 * Whether a template carries spec fields the form does not author.
 *
 * `skills`, `plugins` and `promptTemplate` are not on the form — each is a rich
 * shape of its own, with three artifact-source variants and strict CEL patterns —
 * and an edit deliberately carries them through untouched rather than dropping
 * them.
 *
 * This exists so the form can *say* that. A reader who knows their template has
 * skills and cannot see them would reasonably conclude a save will remove them,
 * and the only way to know otherwise is to be told.
 *
 * In its own module rather than beside the component so the form file exports a
 * component and nothing else — which is what keeps fast refresh working on it.
 */
export function hasUnshownSpecFields(spec: AgentTemplateSpec): boolean {
  return Boolean(
    (spec.skills?.length ?? 0) > 0 ||
      (spec.plugins?.length ?? 0) > 0 ||
      spec.promptTemplate,
  );
}
