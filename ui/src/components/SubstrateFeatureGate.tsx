"use client";

import type { ReactNode } from "react";
import { useSubstrateFeatures } from "@/contexts/SubstrateFeaturesContext";

type SubstrateFeatureGateProps = {
  children: ReactNode;
  /** Shown while capabilities are loading. Defaults to nothing. */
  loadingFallback?: ReactNode;
  /** Shown when substrate is disabled. Defaults to nothing. */
  fallback?: ReactNode;
};

/**
 * Renders children only when Agent Substrate is enabled on the controller.
 * Use for nav items, form sections, or any UI gated on cluster substrate config.
 */
export function SubstrateFeatureGate({
  children,
  loadingFallback = null,
  fallback = null,
}: SubstrateFeatureGateProps) {
  const { enabled, isLoading } = useSubstrateFeatures();

  if (isLoading) {
    return <>{loadingFallback}</>;
  }
  if (!enabled) {
    return <>{fallback}</>;
  }
  return <>{children}</>;
}
