import type { ComponentType } from "react";

/** What an app-icon replacement is told about the space it has. */
export interface ExtensionAppIconProps {
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
export interface ExtensionBranding {
  /**
   * Replaces the application's wordmark. Supplied whole, like every other
   * contribution: an image URL would only work until someone needed two of them
   * at different sizes.
   */
  AppIcon?: ComponentType<ExtensionAppIconProps>;
  /** Product name, used for the document title. */
  appName?: string;
  /**
   * Replaces the tab icon. A URL and not a component, unlike `AppIcon`: the browser
   * loads this itself from a `<link>`, so there is nothing for us to render.
   */
  faviconUrl?: string;
}

/**
 * Applies the title and tab icon a distribution asked for.
 *
 * Takes the merged branding rather than the install, so the "later extension wins"
 * rule is applied once, where every other singular capability applies it — see
 * `selectors.ts`.
 *
 * Both are left alone when unset rather than reset to a default: the document already
 * carries this application's own, and writing them unconditionally would mean every
 * extension had to restate the branding it was happy with.
 */
export function applyExtensionBranding(branding: ExtensionBranding | undefined): void {
  if (branding?.appName) document.title = branding.appName;
  if (!branding?.faviconUrl) return;

  // The tag `index.html` ships, or a fresh one if a host page dropped it — a missing
  // icon is not a reason to leave the extension's branding unapplied.
  const link =
    document.querySelector<HTMLLinkElement>("link[data-app-favicon]") ??
    document.head.appendChild(Object.assign(document.createElement("link"), { rel: "icon" }));
  link.href = branding.faviconUrl;
}
