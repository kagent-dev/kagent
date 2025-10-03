"use client";

import React, { createContext, useContext, useState, useEffect, ReactNode } from "react";

interface SettingsContextType {
  autoRefreshEnabled: boolean;
  autoRefreshInterval: number; // in milliseconds
  setAutoRefreshEnabled: (enabled: boolean) => void;
  setAutoRefreshInterval: (interval: number) => void;
  // SWR configuration helpers
  getSwrConfig: () => {
    refreshInterval?: number;
    revalidateOnFocus: boolean;
    revalidateOnReconnect: boolean;
  };
}

const SettingsContext = createContext<SettingsContextType | undefined>(undefined);

export function useSettings() {
  const context = useContext(SettingsContext);
  if (context === undefined) {
    throw new Error("useSettings must be used within a SettingsProvider");
  }
  return context;
}

interface SettingsProviderProps {
  children: ReactNode;
}

// Default settings
const DEFAULT_AUTO_REFRESH_ENABLED = true;
const DEFAULT_AUTO_REFRESH_INTERVAL = 30000; // 30 seconds

export function SettingsProvider({ children }: SettingsProviderProps) {
  const [autoRefreshEnabled, setAutoRefreshEnabledState] = useState(DEFAULT_AUTO_REFRESH_ENABLED);
  const [autoRefreshInterval, setAutoRefreshIntervalState] = useState(DEFAULT_AUTO_REFRESH_INTERVAL);
  const [isClient, setIsClient] = useState(false);

  // Set isClient to true after hydration to avoid SSR mismatch
  useEffect(() => {
    setIsClient(true);
  }, []);

  // Load settings from localStorage on mount (only on client)
  useEffect(() => {
    if (isClient) {
      const savedAutoRefreshEnabled = localStorage.getItem('kagent.autoRefreshEnabled');
      const savedAutoRefreshInterval = localStorage.getItem('kagent.autoRefreshInterval');

      if (savedAutoRefreshEnabled !== null) {
        setAutoRefreshEnabledState(JSON.parse(savedAutoRefreshEnabled));
      }

      if (savedAutoRefreshInterval !== null) {
        setAutoRefreshIntervalState(parseInt(savedAutoRefreshInterval, 10));
      }
    }
  }, [isClient]);

  const setAutoRefreshEnabled = (enabled: boolean) => {
    setAutoRefreshEnabledState(enabled);
    if (isClient) {
      localStorage.setItem('kagent.autoRefreshEnabled', JSON.stringify(enabled));
    }
  };

  const setAutoRefreshInterval = (interval: number) => {
    setAutoRefreshIntervalState(interval);
    if (isClient) {
      localStorage.setItem('kagent.autoRefreshInterval', interval.toString());
    }
  };

  const getSwrConfig = () => ({
    refreshInterval: (isClient && autoRefreshEnabled) ? autoRefreshInterval : undefined,
    revalidateOnFocus: true,
    revalidateOnReconnect: true,
  });

  const value = {
    autoRefreshEnabled: isClient ? autoRefreshEnabled : false, // Disable auto-refresh until client-side
    autoRefreshInterval,
    setAutoRefreshEnabled,
    setAutoRefreshInterval,
    getSwrConfig,
  };

  return (
    <SettingsContext.Provider value={value}>
      {children}
    </SettingsContext.Provider>
  );
}