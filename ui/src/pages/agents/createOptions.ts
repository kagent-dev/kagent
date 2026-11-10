import type { CreatableKind } from "@/appExtensions/extensionPoints";
import { paths } from "@/router/routes";

export type CreateOption = {
  kind: CreatableKind;
  to: string;
  tagline: string;
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
    tagline: "Run a template on a harness. Chats belong to an agent.",
    testId: "agents-new",
  },
  AgentTemplate: {
    kind: "AgentTemplate",
    to: paths.agentTemplateNew,
    tagline: "Reusable behavior: the model, prompt and tools.",
    testId: "agents-new-template",
  },
  Harness: {
    kind: "Harness",
    to: paths.harnessNew,
    tagline: "Runtime policy: the image and worker pool agents run on.",
    testId: "agents-new-harness",
  },
};
