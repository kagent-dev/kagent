"use client";

import { createContext, useContext, ReactNode } from "react";

const ReadOnlyContext = createContext(false);

export function ReadOnlyProvider({ readOnly, children }: { readOnly: boolean; children: ReactNode }) {
  return <ReadOnlyContext.Provider value={readOnly}>{children}</ReadOnlyContext.Provider>;
}

export function useReadOnly() {
  return useContext(ReadOnlyContext);
}
