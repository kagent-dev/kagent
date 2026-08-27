import type { ComponentType } from "react";

/** What an app-icon replacement is told about the space it has. */
export interface VendorAppIconProps {
  /**
   * True when the shell is showing its narrow form. A mark-only logo belongs
   * here; a stacked wordmark does not fit.
   */
  collapsed: boolean;
}

/**
 * The product's identity, wherever the shell states it.
 *
 * Separate from `theme` because it is not styling: a different logo is not a
 * different colour, and a distribution shipping under its own name needs to say
 * so in the shell, the document title and the tab. Separate from `shell` because
 * it should not cost a layout replacement — a product happy with this
 * application's chrome may still want its own mark on it.
 */
export interface VendorBranding {
  /**
   * Replaces the application's wordmark. Supplied whole, like every other
   * contribution: an image URL would only work until someone needed two of them
   * at different sizes.
   */
  AppIcon?: ComponentType<VendorAppIconProps>;
  /** Product name, used for the document title. */
  appName?: string;
}

/** Applies the document title a distribution asked for. */
export function applyVendorDocumentTitle(
  branding: VendorBranding | undefined,
): void {
  if (branding?.appName) document.title = branding.appName;
}
