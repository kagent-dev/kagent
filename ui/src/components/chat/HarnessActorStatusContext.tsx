"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  getAgentHarnessSessionStatus,
  type AgentHarnessSessionState,
} from "@/app/actions/agentHarnessSession";

const STATUS_POLL_MS = 12000;

type HarnessActorStatusContextValue = {
  state: AgentHarnessSessionState | undefined;
  setState: (state: AgentHarnessSessionState) => void;
  refresh: () => Promise<void>;
};

const HarnessActorStatusContext = createContext<HarnessActorStatusContextValue | undefined>(undefined);

export function HarnessActorStatusProvider({
  namespace,
  harnessName,
  sessionId,
  enabled,
  children,
}: {
  namespace: string;
  harnessName: string;
  sessionId?: string;
  enabled: boolean;
  children: ReactNode;
}) {
  const [state, setState] = useState<AgentHarnessSessionState | undefined>(undefined);

  const refresh = useCallback(async () => {
    if (!enabled || !sessionId) return;
    const response = await getAgentHarnessSessionStatus(namespace, harnessName, sessionId);
    setState(response.data?.state ?? "missing");
  }, [enabled, harnessName, namespace, sessionId]);

  useEffect(() => {
    if (!enabled || !sessionId) return;
    const initial = setTimeout(() => void refresh(), 0);
    const interval = setInterval(() => void refresh(), STATUS_POLL_MS);
    return () => {
      clearTimeout(initial);
      clearInterval(interval);
    };
  }, [enabled, refresh, sessionId]);

  const value = useMemo(() => ({ state, setState, refresh }), [state, refresh]);

  return (
    <HarnessActorStatusContext.Provider value={value}>
      {children}
    </HarnessActorStatusContext.Provider>
  );
}

export function useHarnessActorStatus(): HarnessActorStatusContextValue | undefined {
  return useContext(HarnessActorStatusContext);
}
