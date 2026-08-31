import { createContext, useContext } from "react";

/** State the example extension wants available app-wide, via its own provider. */
export interface ExampleTenant {
  tenantId: string;
  plan: string;
}

export const ExampleTenantContext = createContext<ExampleTenant>({
  tenantId: "unknown",
  plan: "unknown",
});

export function useExampleTenant(): ExampleTenant {
  return useContext(ExampleTenantContext);
}
