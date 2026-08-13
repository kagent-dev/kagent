import { listPromptTemplates } from "@/app/actions/promptTemplates";
import { PromptsPageClient } from "@/components/prompts/PromptsPageClient";

const DEFAULT_PROMPTS_NAMESPACE = "kagent";

export default async function PromptsPage({
  searchParams,
}: {
  searchParams: Promise<{ namespace?: string }>;
}) {
  const params = await searchParams;
  const namespace = params.namespace?.trim() || DEFAULT_PROMPTS_NAMESPACE;
  const response = await listPromptTemplates(namespace);
  return (
    <PromptsPageClient
      namespace={namespace}
      items={response.data ?? []}
      error={response.error ?? null}
    />
  );
}
