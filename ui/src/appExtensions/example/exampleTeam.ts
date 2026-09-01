import type { ExtensionFormPayload } from "@/appExtensions";

/** Where this extension's CRD keeps the value — nowhere near the field's own id. */
export const TEAM_ANNOTATION = "example.com/team";

export const TEAMS = ["platform", "research", "support"] as const;

export type Team = (typeof TEAMS)[number];

/**
 * The field's value type. A required select starts empty rather than
 * pre-answered, so `""` is a real state the value can hold — and the reason
 * this field's `validate` is reachable at all.
 */
export type TeamValue = Team | "";

export function isTeam(value: unknown): value is Team {
  return typeof value === "string" && (TEAMS as readonly string[]).includes(value);
}

/** Reads `metadata.annotations` out of a payload without assuming it exists. */
export function readAnnotations(
  payload: ExtensionFormPayload,
): Record<string, unknown> {
  const metadata = payload.metadata;
  if (typeof metadata !== "object" || metadata === null) return {};
  const annotations = (metadata as Record<string, unknown>).annotations;
  if (typeof annotations !== "object" || annotations === null) return {};
  return annotations as Record<string, unknown>;
}
