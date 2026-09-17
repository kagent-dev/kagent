import type { ReactNode } from "react";
import { useAppExtensions } from "./hooks";
import { extensionProviders } from "./selectors";

interface ExtensionProvidersProps {
  children: ReactNode;
}

/**
 * Wraps the app in every installed extension's own React context providers.
 *
 * Composed by folding from the end, so the first entry ends up outermost — the
 * order it reads in the config is the order it nests. With several extensions
 * installed, the earlier extension's providers sit outside the later one's, which
 * is the same rule applied one level up.
 */
export function ExtensionProviders({ children }: ExtensionProvidersProps) {
  const providers = extensionProviders(useAppExtensions());

  return (
    <>
      {providers.reduceRight(
        (tree, Provider) => <Provider>{tree}</Provider>,
        children,
      )}
    </>
  );
}
