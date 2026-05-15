import type { OpenClawSandboxFormValidationError } from "@/lib/openClawSandboxForm";

export interface AgentFormValidationErrors {
  name?: string;
  namespace?: string;
  description?: string;
  type?: string;
  systemPrompt?: string;
  model?: string;
  knowledgeSources?: string;
  tools?: string;
  skills?: string;
  memoryModel?: string;
  memoryTtl?: string;
  serviceAccountName?: string;
  promptSources?: string;
  openClawSandbox?: OpenClawSandboxFormValidationError;
}
