import { getUiRuntimeConfig } from "@/app/actions/config";
import { listPromptTemplates } from "@/app/actions/promptTemplates";
import { PromptsPageClient } from "@/components/prompts/PromptsPageClient";

export default async function PromptsPage({
  searchParams,
}: {
  searchParams: Promise<{ namespace?: string }>;
}) {
  const [{ defaultNamespace }, params] = await Promise.all([
    getUiRuntimeConfig(),
    searchParams,
  ]);
  const namespace = params.namespace?.trim() || defaultNamespace;
  const response = await listPromptTemplates(namespace);
  return (
    <PromptsPageClient
      namespace={namespace}
      items={response.data ?? []}
      error={response.error ?? null}
    />
  );
}
