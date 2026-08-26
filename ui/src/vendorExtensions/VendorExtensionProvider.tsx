import { useMemo } from "react";
import type { ReactNode } from "react";
import { VendorExtensionContext } from "./context";
import { validateVendorExtensionConfig } from "./validateConfig";
import type { VendorExtensionConfig } from "./types";

interface VendorExtensionProviderProps {
  config: VendorExtensionConfig;
  /** Core route paths, so a vendor route colliding with one is caught. */
  reservedPaths?: readonly string[];
  children: ReactNode;
}

/**
 * Publishes the global vendor extension config to the tree.
 *
 * Validation runs here, during the first render, so a bad config fails at boot
 * with a list of problems rather than as a component that quietly never
 * appears.
 */
export function VendorExtensionProvider({
  config,
  reservedPaths,
  children,
}: VendorExtensionProviderProps) {
  useMemo(
    () => validateVendorExtensionConfig(config, reservedPaths),
    [config, reservedPaths],
  );

  return (
    <VendorExtensionContext.Provider value={config}>
      {children}
    </VendorExtensionContext.Provider>
  );
}
