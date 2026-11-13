import type { CreatableKind } from "@/appExtensions/extensionPoints";
import { paths } from "@/router/routes";

export type CreateOption = {
  kind: CreatableKind;
  to: string;
  /** The one wording for this kind: the modal row and the overview diagram both show it. */
  description: string;
  testId: string;
};

/**
 * The three things the Agents page can create, shared by the header modal and the
 * overview diagram. Keyed by kind, so a missing or misspelled one fails to compile; the
 * key order is the order the modal lists them in.
 */
export const CREATE_OPTIONS: Record<CreatableKind, CreateOption> = {
  Agent: {
    kind: "Agent",
    to: paths.agentNew,
    description: "Inline or shared behavior on a platform-managed harness.",
    testId: "agents-new",
  },
  AgentTemplate: {
    kind: "AgentTemplate",
    to: paths.agentTemplateNew,
    description: "The model, prompt and tools.",
    testId: "agents-new-template",
  },
  Harness: {
    kind: "Harness",
    to: paths.harnessNew,
    description: "The runtime, image and worker pool.",
    testId: "agents-new-harness",
  },
};
