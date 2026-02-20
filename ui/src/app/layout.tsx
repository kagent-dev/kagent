import type { Metadata } from "next";
import { Geist } from "next/font/google";
import "./globals.css";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentsProvider } from "@/components/AgentsProvider";
import { Header } from "@/components/Header";
import { Footer } from "@/components/Footer";
import { ThemeProvider } from "@/components/ThemeProvider";
import { Toaster } from "@/components/ui/sonner";
import { AppInitializer } from "@/components/AppInitializer";
import { ReadOnlyProvider } from "@/components/ReadOnlyProvider";

export const dynamic = "force-dynamic";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "kagent.dev",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  const readOnly = process.env.NEXT_PUBLIC_READ_ONLY === "true";

  return (
    <html lang="en" className="">
      <body className={`${geistSans.className} flex flex-col h-screen overflow-hidden`}>
        <ReadOnlyProvider readOnly={readOnly}>
          <TooltipProvider>
            <AgentsProvider>
              <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
                <AppInitializer>
                  <Header />
                  <main className="flex-1 overflow-y-scroll w-full mx-auto">{children}</main>
                  <Footer />
                </AppInitializer>
                <Toaster richColors/>
              </ThemeProvider>
            </AgentsProvider>
          </TooltipProvider>
        </ReadOnlyProvider>
      </body>
    </html>
  );
}
