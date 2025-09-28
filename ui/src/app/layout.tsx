import type { Metadata } from "next";
import { Geist } from "next/font/google";
import "./globals.css";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentsProvider } from "@/components/AgentsProvider";
import { AuthProvider } from "@/contexts/AuthContext";
import { OrganizationProvider } from "@/contexts/OrganizationContext";
import { Header } from "@/components/Header";
import { Footer } from "@/components/Footer";
import { ThemeProvider } from "@/components/ThemeProvider";
import { Toaster } from "@/components/ui/sonner";
import { AppInitializer } from "@/components/AppInitializer";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "adolphe.ai - Enterprise AI Agent Platform",
  description: "Deploy autonomous AI agents for enterprise automation. Orchestrate models, tools, and workflows with enterprise-grade security and scalability.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className={`${geistSans.className} flex flex-col min-h-screen`}>
        <TooltipProvider>
          <AgentsProvider>
            <AuthProvider>
              <OrganizationProvider>
                <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
                  <AppInitializer>
                    <Header />
                    <main className="flex-1 w-full">
                      <div className="w-full">
                        {children}
                      </div>
                    </main>
                    <Footer />
                  </AppInitializer>
                  <Toaster richColors/>
                </ThemeProvider>
              </OrganizationProvider>
            </AuthProvider>
          </AgentsProvider>
        </TooltipProvider>
      </body>
    </html>
  );
}
