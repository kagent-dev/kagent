export const A2A_METADATA = {
  outputSchemaSha256: "kagent.dev/a2a/output-schema-sha256",
  partType: "kagent.dev/a2a/part-type",
  timelinePosition: "kagent.dev/a2a/timeline-position",
} as const;

export function metadataString(
  metadata: Record<string, unknown> | undefined,
  key: string,
): string | undefined {
  const value = metadata?.[key];
  return typeof value === "string" ? value : undefined;
}
