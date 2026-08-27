import type { VendorFormPayload } from "@/vendorExtensions";

/** Where this vendor's CRD keeps the value — nowhere near the field's own id. */
export const TIER_ANNOTATION = "example.com/compliance-tier";

export const TIERS = ["standard", "regulated", "restricted"] as const;

export type ComplianceTier = (typeof TIERS)[number];

/**
 * The field's value type. A required select starts empty rather than
 * pre-answered, so `""` is a real state the value can hold — and the reason
 * this field's `validate` is reachable at all.
 */
export type ComplianceTierValue = ComplianceTier | "";

export function isComplianceTier(value: unknown): value is ComplianceTier {
  return typeof value === "string" && (TIERS as readonly string[]).includes(value);
}

/** Reads `metadata.annotations` out of a payload without assuming it exists. */
export function readAnnotations(
  payload: VendorFormPayload,
): Record<string, unknown> {
  const metadata = payload.metadata;
  if (typeof metadata !== "object" || metadata === null) return {};
  const annotations = (metadata as Record<string, unknown>).annotations;
  if (typeof annotations !== "object" || annotations === null) return {};
  return annotations as Record<string, unknown>;
}
