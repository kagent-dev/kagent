"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { listPromptTemplates } from "@/app/actions/promptTemplates";
import type { PromptTemplateSummary } from "@/types";
import { toast } from "sonner";
import { AppPageFrame } from "@/components/layout/AppPageFrame";
import { PromptLibrariesPanel } from "@/components/prompts/PromptLibrariesPanel";

const DEFAULT_PROMPTS_NAMESPACE = "kagent";

export default function PromptsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const namespace = searchParams.get("namespace") ?? "";
  const [items, setItems] = useState<PromptTemplateSummary[] | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (searchParams.get("namespace")) {
      return;
    }
    const q = new URLSearchParams(searchParams.toString());
    q.set("namespace", DEFAULT_PROMPTS_NAMESPACE);
    router.replace(`/prompts?${q.toString()}`, { scroll: false });
  }, [router, searchParams]);

  const syncNsToUrl = useCallback(
    (ns: string) => {
      const q = new URLSearchParams(searchParams.toString());
      if (ns) {
        q.set("namespace", ns);
      } else {
        q.delete("namespace");
      }
      router.replace(`/prompts?${q.toString()}`, { scroll: false });
    },
    [router, searchParams],
  );

  const handleNamespaceChange = (ns: string) => {
    syncNsToUrl(ns);
  };

  useEffect(() => {
    if (!namespace) {
      return;
    }
    let cancelled = false;
    (async () => {
      setLoading(true);
      const res = await listPromptTemplates(namespace);
      if (cancelled) {
        return;
      }
      if (res.error || !res.data) {
        toast.error(res.error || "Could not load prompt libraries");
        setItems([]);
      } else {
        setItems(res.data);
      }
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [namespace]);

  return (
    <AppPageFrame ariaLabelledBy="prompts-page-title" mainClassName="mx-auto max-w-6xl px-4 py-10 sm:px-6">
      <PromptLibrariesPanel
        namespace={namespace}
        loading={loading}
        items={items}
        onNamespaceChange={handleNamespaceChange}
      />
    </AppPageFrame>
  );
}
