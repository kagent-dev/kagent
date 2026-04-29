"use client";

import * as React from "react";
import { skipToContentLinkClassName } from "@/lib/skipToContent";
import { cn } from "@/lib/utils";

type AppPageFrameProps = {
  children: React.ReactNode;
  /** Defaults to `page-main` (must match skip-link `href`). */
  mainId?: string;
  className?: string;
  /** Subtle page background matching Create / Edit Agent (`to-muted/15` gradient). */
  surface?: "studio" | "plain";
  /** Optional extra classes on `<main>`. */
  mainClassName?: string;
  /** `aria-labelledby` — set to your page `<h1>` id. */
  ariaLabelledBy?: string;
  showSkipLink?: boolean;
};

/**
 * Shared layout: skip link, `touch-manipulation`, and a focusable `<main>` landmark (Web Interface Guidelines).
 */
export function AppPageFrame({
  children,
  mainId = "page-main",
  className,
  surface = "studio",
  mainClassName,
  ariaLabelledBy,
  showSkipLink = true,
}: AppPageFrameProps) {
  return (
    <div
      className={cn(
        "relative min-h-screen touch-manipulation",
        surface === "studio" && "bg-gradient-to-b from-background via-background to-muted/15",
        className,
      )}
    >
      {showSkipLink ? (
        <a href={`#${mainId}`} className={skipToContentLinkClassName}>
          Skip to content
        </a>
      ) : null}
      <main
        id={mainId}
        className={cn("scroll-mt-8 outline-none", mainClassName)}
        tabIndex={-1}
        {...(ariaLabelledBy ? { "aria-labelledby": ariaLabelledBy } : {})}
      >
        {children}
      </main>
    </div>
  );
}
