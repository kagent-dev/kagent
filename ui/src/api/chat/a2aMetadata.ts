export const A2A_METADATA = {
  outputSchemaSha256: "kagent.dev/a2a/output-schema-sha256",
  partType: "kagent.dev/a2a/part-type",
  timelinePosition: "kagent.dev/a2a/timeline-position",
} as const;

/**
 * The versioned token usage extension. Its payload is the metadata key on a
 * task's terminal status update. Version 1 is emitted whether or not a call
 * activates it; activating it anyway keeps a later gated version a no-op here.
 */
export const USAGE_EXTENSION_URI = "https://kagent.dev/extensions/usage/v1";

export function metadataString(
  metadata: Record<string, unknown> | undefined,
  key: string,
): string | undefined {
  const value = metadata?.[key];
  return typeof value === "string" ? value : undefined;
}
