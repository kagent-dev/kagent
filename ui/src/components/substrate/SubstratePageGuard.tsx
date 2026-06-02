"use client";

import { useEffect, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { useSubstrateFeatures } from "@/contexts/SubstrateFeaturesContext";

type SubstratePageGuardProps = {
  children: ReactNode;
};

/** Redirects away from substrate routes when the feature is not enabled on the cluster. */
export function SubstratePageGuard({ children }: SubstratePageGuardProps) {
  const router = useRouter();
  const { enabled, isLoading } = useSubstrateFeatures();

  useEffect(() => {
    if (!isLoading && !enabled) {
      router.replace("/");
    }
  }, [enabled, isLoading, router]);

  if (isLoading) {
    return (
      <div className="mx-auto max-w-6xl px-4 py-8 text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }

  if (!enabled) {
    return null;
  }

  return <>{children}</>;
}
