import type { AgentFormValidationErrors } from "./agent-form-types";

const FOCUS_ORDER: (keyof AgentFormValidationErrors)[] = [
  "name",
  "namespace",
  "description",
  "systemPrompt",
  "model",
  "memoryModel",
  "memoryTtl",
  "serviceAccountName",
  "promptSources",
  "tools",
  "skills",
  "knowledgeSources",
  "type",
];

function focusElementById(id: string) {
  const el = document.getElementById(id);
  if (!el) {
    return false;
  }
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLButtonElement) {
    el.focus();
  } else {
    el.setAttribute("tabindex", "-1");
    el.focus();
  }
  const reduceMotion = typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  el.scrollIntoView({ block: "nearest", behavior: reduceMotion ? "auto" : "smooth" });
  return true;
}

/**
 * Move focus to the first field with a validation error (Web Interface Guidelines: focus first error on submit).
 */
export function focusFirstFormError(
  errors: AgentFormValidationErrors,
  options: { byoSectionsActive: boolean },
) {
  for (const key of FOCUS_ORDER) {
    if (!errors[key]) {
      continue;
    }
    if (key === "model" && options.byoSectionsActive) {
      if (focusElementById("agent-field-byo-image")) {
        return;
      }
      continue;
    }
    if (key === "serviceAccountName") {
      if (focusElementById("agent-field-service-account") || focusElementById("agent-field-service-account-byo")) {
        return;
      }
    }
    const idMap: Partial<Record<keyof AgentFormValidationErrors, string>> = {
      name: "agent-field-name",
      namespace: "agent-field-namespace",
      description: "agent-desc",
      systemPrompt: "agent-field-system",
      model: "agent-field-model",
      memoryModel: "agent-field-memory-model",
      memoryTtl: "agent-field-memory-ttl",
      skills: "section-skills",
      tools: "section-tools",
    };
    const id = idMap[key];
    if (id && focusElementById(id)) {
      return;
    }
  }
}
