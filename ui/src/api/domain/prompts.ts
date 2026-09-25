/**
 * Prompt library domain models.
 *
 * A PromptTemplate is a namespaced bag of named fragments that agents pull into
 * their system message with `{{include "alias/key"}}`.
 */

/** One row of `PromptTemplateService.ListPromptTemplates`. */
export interface PromptTemplateSummary {
  namespace: string;
  name: string;
  keyCount: number;
  /** Fragment keys, for the include picker. */
  keys?: string[];
}

/** `PromptTemplateService.GetPromptTemplate` — the fragments themselves. */
export interface PromptTemplateDetail {
  namespace: string;
  name: string;
  data: Record<string, string>;
}

export interface CreatePromptTemplateRequest {
  namespace: string;
  name: string;
  data: Record<string, string>;
}

export interface UpdatePromptTemplateRequest {
  data: Record<string, string>;
}
