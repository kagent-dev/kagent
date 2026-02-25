import type { Metadata } from "next";
import { Geist } from "next/font/google";
import "./globals.css";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentsProvider } from "@/components/AgentsProvider";
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar";
import { AppSidebar } from "@/components/sidebars/AppSidebar";
import { ThemeProvider } from "@/components/ThemeProvider";
import { Toaster } from "@/components/ui/sonner";
import { AppInitializer } from "@/components/AppInitializer";
import { NamespaceProvider } from "@/lib/namespace-context";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "kagent.dev",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <TooltipProvider>
      <AgentsProvider>
        <NamespaceProvider>
          <html lang="en" className="">
            <body className={`${geistSans.className} flex h-screen overflow-hidden`}>
              <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
                <AppInitializer>
                  <SidebarProvider>
                    <AppSidebar />
                    <SidebarInset className="flex-1 overflow-y-auto">
                      {children}
                    </SidebarInset>
                  </SidebarProvider>
                </AppInitializer>
                <Toaster richColors/>
              </ThemeProvider>
            </body>
          </html>
        </NamespaceProvider>
      </AgentsProvider>
    </TooltipProvider>
  );
}
