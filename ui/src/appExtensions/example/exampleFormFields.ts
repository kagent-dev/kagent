import { defineExtensionFormField } from "@/appExtensions";
import { ExampleTeamField } from "./ExampleTeamField";
import { TEAM_ANNOTATION, isTeam, readAnnotations } from "./exampleTeam";
import type { TeamValue } from "./exampleTeam";

/**
 * A field the extension adds to the core "new agent" form.
 *
 * The mappers are the interesting half: the value is rendered as a plain
 * select but lands in the request as an annotation on the agent's metadata,
 * which is where this extension's CRD reads it from.
 *
 * It starts unset on purpose. A required field that defaults to a valid answer
 * can never fail its own validation, so the example would document a rule it
 * never demonstrates.
 */
export const exampleTeamField = defineExtensionFormField<TeamValue>({
  id: "exampleTeam",
  formId: "app_agents_agentNew_agentForm",
  order: 10,
  Component: ExampleTeamField,
  defaultValue: "",

  fromPayload: (payload) => {
    const raw = readAnnotations(payload)[TEAM_ANNOTATION];
    return isTeam(raw) ? raw : "";
  },

  toPayload: (payload, value) => {
    const metadata =
      typeof payload.metadata === "object" && payload.metadata !== null
        ? (payload.metadata as Record<string, unknown>)
        : {};

    // An unanswered field writes nothing, so a rejected submit does not leave a
    // blank annotation behind on the payload.
    if (!isTeam(value)) return payload;

    return {
      ...payload,
      metadata: {
        ...metadata,
        annotations: { ...readAnnotations(payload), [TEAM_ANNOTATION]: value },
      },
    };
  },

  validate: (value) => (isTeam(value) ? undefined : "Pick a team"),
});
