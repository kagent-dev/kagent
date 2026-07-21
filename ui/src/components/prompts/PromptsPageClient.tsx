"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { ErrorState } from "@/components/ErrorState";
import { PromptLibrariesPanel } from "@/components/prompts/PromptLibrariesPanel";
import type { PromptTemplateSummary } from "@/types";

export function PromptsPageClient({
  namespace,
  items,
  error,
}: {
  namespace: string;
  items: PromptTemplateSummary[];
  error: string | null;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();

  const handleNamespaceChange = (nextNamespace: string) => {
    const query = new URLSearchParams(searchParams.toString());
    if (nextNamespace) query.set("namespace", nextNamespace);
    else query.delete("namespace");
    router.replace(`/prompts${query.size ? `?${query.toString()}` : ""}`, { scroll: false });
  };

  if (error) return <ErrorState message={error} />;

  return (
    <AppPageFrame ariaLabelledBy="prompts-page-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <PromptLibrariesPanel
        namespace={namespace}
        loading={false}
        items={items}
        onNamespaceChange={handleNamespaceChange}
      />
    </AppPageFrame>
  );
}
