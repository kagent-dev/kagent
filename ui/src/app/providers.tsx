"use client";

import type { ReactNode } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentsProvider } from "@/components/AgentsProvider";
import { AuthProvider } from "@/contexts/AuthContext";
import { SidebarProvider } from "@/components/ui/sidebar";
import { ThemeProvider } from "@/components/ThemeProvider";
import { AppInitializer } from "@/components/AppInitializer";
import { NamespaceProvider } from "@/lib/namespace-context";
import { Toaster } from "@/components/ui/sonner";
import { SubstrateFeaturesProvider } from "@/contexts/SubstrateFeaturesContext";

interface ProvidersProps {
  children: ReactNode;
  sidebarDefaultOpen: boolean;
}

/**
 * Single client boundary for the root layout. Keeps `app/layout.tsx` a Server
 * Component and centralizes provider ordering. `sidebarDefaultOpen` is read on
 * the server from the `sidebar_state` cookie so the sidebar renders in its
 * persisted state without a hydration flash.
 */
export function Providers({ children, sidebarDefaultOpen }: ProvidersProps) {
  return (
    <TooltipProvider>
      <AgentsProvider>
        <SubstrateFeaturesProvider>
          <AuthProvider>
            <NamespaceProvider>
              <ThemeProvider
                attribute="class"
                defaultTheme="system"
                enableSystem
                disableTransitionOnChange
              >
                <AppInitializer>
                  <SidebarProvider defaultOpen={sidebarDefaultOpen}>
                    {children}
                  </SidebarProvider>
                </AppInitializer>
                <Toaster richColors />
              </ThemeProvider>
            </NamespaceProvider>
          </AuthProvider>
        </SubstrateFeaturesProvider>
      </AgentsProvider>
    </TooltipProvider>
  );
}
