"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";

export interface SidebarPluginNav {
  name: string;
  pathPrefix: string;
  displayName: string;
  icon: string;
  section: string;
}

export type SidebarStatus = "ok" | "plugins-failed" | "loading";

interface SidebarStatusContextValue {
  status: SidebarStatus;
  plugins: SidebarPluginNav[];
  retry: () => void;
}

const SidebarStatusContext = createContext<SidebarStatusContextValue | null>(null);

export function useSidebarStatus(): SidebarStatusContextValue {
  const ctx = useContext(SidebarStatusContext);
  if (!ctx) {
    throw new Error("useSidebarStatus must be used within SidebarStatusProvider");
  }
  return ctx;
}

export function SidebarStatusProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<SidebarStatus>("loading");
  const [plugins, setPlugins] = useState<SidebarPluginNav[]>([]);
  const [fetchKey, setFetchKey] = useState(0);

  const load = useCallback(() => {
    setStatus("loading");
    fetch("/api/plugins")
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((res) => {
        setPlugins(res.data ?? []);
        setStatus("ok");
      })
      .catch(() => {
        setStatus("plugins-failed");
      });
  }, []);

  useEffect(() => {
    load();
  }, [load, fetchKey]); // fetchKey changes when retry() is called

  const retry = useCallback(() => {
    setFetchKey((k) => k + 1);
  }, []);

  const value: SidebarStatusContextValue = {
    status,
    plugins,
    retry,
  };

  return (
    <SidebarStatusContext.Provider value={value}>
      {children}
    </SidebarStatusContext.Provider>
  );
}
