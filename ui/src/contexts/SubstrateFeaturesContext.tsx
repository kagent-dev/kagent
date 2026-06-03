"use client";

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { getSubstrateStatus } from "@/app/actions/substrate";

export interface SubstrateFeaturesContextValue {
  /** True when the controller has Agent Substrate configured (ate-api endpoint set). */
  enabled: boolean;
  isLoading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
}

const SubstrateFeaturesContext = createContext<SubstrateFeaturesContextValue | undefined>(
  undefined,
);

export function SubstrateFeaturesProvider({ children }: { children: ReactNode }) {
  const [enabled, setEnabled] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refetch = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await getSubstrateStatus();
      if (result.error || !result.data) {
        setEnabled(false);
        setError(result.error ?? "Failed to load substrate features");
        return;
      }
      setEnabled(result.data.enabled);
    } catch (e) {
      setEnabled(false);
      setError(e instanceof Error ? e.message : "Failed to load substrate features");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    void refetch();
  }, [refetch]);

  const value = useMemo(
    () => ({ enabled, isLoading, error, refetch }),
    [enabled, isLoading, error, refetch],
  );

  return (
    <SubstrateFeaturesContext.Provider value={value}>{children}</SubstrateFeaturesContext.Provider>
  );
}

export function useSubstrateFeatures(): SubstrateFeaturesContextValue {
  const context = useContext(SubstrateFeaturesContext);
  if (context === undefined) {
    throw new Error("useSubstrateFeatures must be used within a SubstrateFeaturesProvider");
  }
  return context;
}

/** True after the initial probe finishes and substrate is enabled on the cluster. */
export function useSubstrateEnabled(): boolean {
  const { enabled, isLoading } = useSubstrateFeatures();
  return !isLoading && enabled;
}

/** For Storybook/tests: inject feature flags without calling the API. */
export function SubstrateFeaturesTestProvider({
  children,
  enabled,
  isLoading = false,
}: {
  children: ReactNode;
  enabled: boolean;
  isLoading?: boolean;
}) {
  const value = useMemo<SubstrateFeaturesContextValue>(
    () => ({
      enabled,
      isLoading,
      error: null,
      refetch: async () => {},
    }),
    [enabled, isLoading],
  );
  return (
    <SubstrateFeaturesContext.Provider value={value}>{children}</SubstrateFeaturesContext.Provider>
  );
}
