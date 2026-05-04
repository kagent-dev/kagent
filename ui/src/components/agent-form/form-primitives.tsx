"use client";

import * as React from "react";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

export function FormSection({
  id,
  title,
  description,
  children,
  className,
}: {
  id?: string;
  title: string;
  description?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section
      id={id}
      className={cn("rounded-lg border border-border/90 bg-card text-card-foreground shadow-sm", className)}
    >
      <header className="border-b border-border/60 px-5 py-4">
        <h2 className="scroll-mt-20 text-balance text-base font-semibold tracking-tight text-foreground">{title}</h2>
        {description ? (
          <p className="mt-1.5 max-w-2xl text-pretty text-sm leading-relaxed text-muted-foreground">{description}</p>
        ) : null}
      </header>
      <div className="space-y-5 p-5">{children}</div>
    </section>
  );
}

export function FieldRoot({ className, children }: { className?: string; children: React.ReactNode }) {
  return <div className={cn("space-y-1.5", className)}>{children}</div>;
}

export function FieldLabel({ htmlFor, className, children }: { htmlFor?: string; className?: string; children: React.ReactNode }) {
  return (
    <Label htmlFor={htmlFor} className={cn("text-sm font-medium", className)}>
      {children}
    </Label>
  );
}

export function FieldHint({ className, children }: { className?: string; children: React.ReactNode }) {
  return <p className={cn("text-xs text-muted-foreground leading-relaxed", className)}>{children}</p>;
}

export function FieldError({ children }: { children: React.ReactNode | null | undefined }) {
  if (children == null || children === "") {
    return null;
  }
  return (
    <p className="text-sm text-destructive mt-1" role="alert">
      {children}
    </p>
  );
}
