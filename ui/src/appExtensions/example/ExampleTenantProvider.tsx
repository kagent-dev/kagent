import type { ReactNode } from "react";
import { ExampleTenantContext } from "./exampleTenant";
import type { ExampleTenant } from "./exampleTenant";

const tenant: ExampleTenant = { tenantId: "example-eu-1", plan: "platform" };

/**
 * An app-level provider contributed through the config's `providers` list. A
 * real extension would wrap its own query client or telemetry here; this one
 * publishes a tenant so the injected page can prove the provider actually ran.
 */
export function ExampleTenantProvider({ children }: { children: ReactNode }) {
  return (
    <ExampleTenantContext.Provider value={tenant}>
      {children}
    </ExampleTenantContext.Provider>
  );
}
