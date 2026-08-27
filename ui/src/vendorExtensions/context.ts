import { createContext } from "react";
import { emptyVendorExtensionConfig } from "./types";
import type { VendorExtensionConfig } from "./types";

/**
 * Carries the one global config. Defaulted to the empty config rather than
 * `undefined` so a component rendered outside the provider — a unit test, a
 * Storybook story — behaves exactly like the no-extension case instead of
 * throwing.
 */
export const VendorExtensionContext = createContext<VendorExtensionConfig>(
  emptyVendorExtensionConfig,
);
