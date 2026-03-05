"use client";

import { createContext, useContext, useState, ReactNode } from "react";

interface NamespaceContextType {
  namespace: string;
  setNamespace: (ns: string) => void;
}

const NamespaceContext = createContext<NamespaceContextType | undefined>(undefined);

export function NamespaceProvider({ children }: { children: ReactNode }) {
  const [namespace, setNamespace] = useState("");
  return (
    <NamespaceContext.Provider value={{ namespace, setNamespace }}>
      {children}
    </NamespaceContext.Provider>
  );
}

export function useNamespace(): NamespaceContextType {
  const context = useContext(NamespaceContext);
  if (!context) {
    throw new Error("useNamespace must be used within a NamespaceProvider");
  }
  return context;
}
