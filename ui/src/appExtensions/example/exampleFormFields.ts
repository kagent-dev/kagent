import { defineExtensionFormField } from "@/appExtensions";
import { ExampleComplianceTierField } from "./ExampleComplianceTierField";
import {
  TIER_ANNOTATION,
  isComplianceTier,
  readAnnotations,
} from "./exampleComplianceTier";
import type { ComplianceTierValue } from "./exampleComplianceTier";

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
export const exampleComplianceTierField = defineExtensionFormField<ComplianceTierValue>({
  id: "exampleComplianceTier",
  formId: "app_agents_agentNew_agentForm",
  order: 10,
  Component: ExampleComplianceTierField,
  defaultValue: "",

  fromPayload: (payload) => {
    const raw = readAnnotations(payload)[TIER_ANNOTATION];
    return isComplianceTier(raw) ? raw : "";
  },

  toPayload: (payload, value) => {
    const metadata =
      typeof payload.metadata === "object" && payload.metadata !== null
        ? (payload.metadata as Record<string, unknown>)
        : {};

    // An unanswered field writes nothing, so a rejected submit does not leave a
    // blank annotation behind on the payload.
    if (!isComplianceTier(value)) return payload;

    return {
      ...payload,
      metadata: {
        ...metadata,
        annotations: { ...readAnnotations(payload), [TIER_ANNOTATION]: value },
      },
    };
  },

  validate: (value) =>
    isComplianceTier(value) ? undefined : "Pick a compliance tier",
});
