export const A2A_METADATA = {
  /** The agent that wrote an artifact; a workflow has one per node. */
  author: "kagent.dev/a2a/author",
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
