import type { ReactNode } from "react";
import { useVendorExtensionConfig } from "./hooks";

interface VendorProvidersProps {
  children: ReactNode;
}

/**
 * Wraps the app in the vendor's own React context providers.
 *
 * Composed by folding from the end, so the first entry in `providers` ends up
 * outermost — the order it reads in the config is the order it nests.
 */
export function VendorProviders({ children }: VendorProvidersProps) {
  const { providers } = useVendorExtensionConfig();

  return (
    <>
      {(providers ?? []).reduceRight(
        (tree, Provider) => <Provider>{tree}</Provider>,
        children,
      )}
    </>
  );
}
