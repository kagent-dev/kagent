import { getModelConfigs } from "@/app/actions/modelConfigs";
import { ModelsPageClient } from "@/components/models/ModelsPageClient";

export default async function ModelsPage() {
  const response = await getModelConfigs();
  return <ModelsPageClient initialModels={response.data ?? []} initialError={response.error ?? null} />;
}
