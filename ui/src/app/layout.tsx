import type { Metadata } from "next";
import { cookies } from "next/headers";
import { Geist } from "next/font/google";
import "./globals.css";
import { Providers } from "./providers";
import { SidebarInset } from "@/components/ui/sidebar";
import { AppSidebar } from "@/components/sidebars/AppSidebar";
import { MobileTopBar } from "@/components/MobileTopBar";

export const metadata: Metadata = {
  title: "kagent.dev | Amdocs.com",
};

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const SIDEBAR_COOKIE_NAME = "sidebar_state";

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const cookieStore = await cookies();
  const sidebarCookie = cookieStore.get(SIDEBAR_COOKIE_NAME)?.value;
  // Default to expanded when the cookie is unset (first visit).
  const sidebarDefaultOpen = sidebarCookie !== "false";

  return (
    <html lang="en" suppressHydrationWarning>
      <body
        suppressHydrationWarning
        className={`${geistSans.className} flex h-screen overflow-hidden`}
      >
        <Providers sidebarDefaultOpen={sidebarDefaultOpen}>
          <AppSidebar />
          <SidebarInset className="flex-1 overflow-y-auto">
            <MobileTopBar />
            {children}
          </SidebarInset>
        </Providers>
      </body>
    </html>
  );
}
