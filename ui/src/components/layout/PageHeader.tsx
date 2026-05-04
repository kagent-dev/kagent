"use client";

import * as React from "react";
import { cn } from "@/lib/utils";

export const pageTitleTextClass = "text-balance text-2xl font-semibold tracking-tight text-foreground sm:text-3xl";

const inlineLinkClass =
  "shrink-0 text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background";

export { inlineLinkClass };

type PageHeaderProps = {
  /** `id` for the `<h1>`; pair with `AppPageFrame` `ariaLabelledBy` */
  titleId: string;
  title: string;
  /** Renders as primary page description (lede) under the title. */
  description?: React.ReactNode;
  /** Extra content inline with the title row (e.g. “View tools” link). */
  afterTitle?: React.ReactNode;
  /** Trailing area on wide screens (e.g. CTA, filters). */
  end?: React.ReactNode;
  className?: string;
  titleClassName?: string;
  isMonospaceTitle?: boolean;
};

/**
 * Page title + lede aligned with the Create Agent flow.
 */
export function PageHeader({
  titleId,
  title,
  description,
  afterTitle,
  end,
  className,
  titleClassName,
  isMonospaceTitle,
}: PageHeaderProps) {
  const hasEnd = end != null;

  return (
    <header
      className={cn(
        "mb-10",
        hasEnd
          ? "flex max-w-none flex-col gap-6 sm:flex-row sm:items-end sm:justify-between"
          : "max-w-2xl",
        className, // e.g. override margin with `mb-8`
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-baseline sm:gap-x-4 sm:gap-y-2">
          <h1
            id={titleId}
            className={cn(
              pageTitleTextClass,
              isMonospaceTitle && "font-mono text-xl sm:text-2xl [overflow-wrap:anywhere] break-words",
              titleClassName,
            )}
            translate={isMonospaceTitle ? "no" : undefined}
          >
            {title}
          </h1>
          {afterTitle}
        </div>
        {description != null && description !== false ? (
          <div className="mt-2 max-w-2xl text-pretty text-sm leading-relaxed text-muted-foreground [&_code]:rounded [&_code]:bg-muted [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-xs [&_kbd]:font-mono [&_kbd]:text-xs">
            {description}
          </div>
        ) : null}
      </div>
      {end ? <div className="flex w-full min-w-0 flex-col gap-3 sm:w-auto sm:shrink-0 sm:items-end">{end}</div> : null}
    </header>
  );
}
