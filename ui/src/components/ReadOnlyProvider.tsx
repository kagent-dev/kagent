"use client";

import React, { createContext, useContext } from "react";

const ReadOnlyContext = createContext<boolean>(false);

export function ReadOnlyProvider({ children }: { children: React.ReactNode }) {
  const isReadOnly = process.env.NEXT_PUBLIC_READ_ONLY === "true";

  return (
    <ReadOnlyContext.Provider value={isReadOnly}>
      {children}
    </ReadOnlyContext.Provider>
  );
}

export function useReadOnly(): boolean {
  return useContext(ReadOnlyContext);
}
