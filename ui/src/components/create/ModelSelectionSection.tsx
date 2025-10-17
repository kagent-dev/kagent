import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { ModelConfig } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface ModelSelectionSectionProps {
  allModels: ModelConfig[];
  selectedModel: Partial<ModelConfig> | null;
  setSelectedModel: (model: Partial<ModelConfig> | null) => void;
  error?: string;
  isSubmitting: boolean;
  onChange?: (modelRef: string) => void;
  agentNamespace?: string;
}

export const ModelSelectionSection = ({
  allModels,
  selectedModel,
  setSelectedModel,
  error,
  isSubmitting,
  onChange,
  agentNamespace
}: ModelSelectionSectionProps) => {
  const getModelNamespace = (modelRef: string): string => {
    try {
      return k8sRefUtils.fromRef(modelRef).namespace;
    } catch {
      return 'default';
    }
  };

  const isModelSelectable = (modelRef: string): boolean => {
    if (!agentNamespace) return true;
    const modelNamespace = getModelNamespace(modelRef);
    return modelNamespace === agentNamespace;
  };

  return (
    <>
      <label className="text-base mb-2 block font-bold">Model</label>
      <p className="text-xs mb-2 block text-muted-foreground">
        This is the model that will be used to generate the agent&apos;s responses.
        {agentNamespace && (
          <span className="block mt-1">
            Only models from the <strong>{agentNamespace}</strong> namespace are selectable.
          </span>
        )}
      </p>
      <Select
        key={`model-select-${agentNamespace}`}
        value={selectedModel?.ref || ""}
        disabled={isSubmitting || allModels.length === 0}
        onValueChange={(value) => {
          const model = allModels.find((m) => m.ref === value);
          if (model && isModelSelectable(model.ref)) {
            setSelectedModel(model);
            onChange?.(model.ref);
          }
        }}
      >
        <SelectTrigger className={`${error ? "border-red-500" : ""}`}>
          <SelectValue placeholder="Select a model" />
        </SelectTrigger>
        <SelectContent>
          {allModels.map((model, idx) => {
            const selectable = isModelSelectable(model.ref);
            const modelNamespace = getModelNamespace(model.ref);
            const isDifferentNamespace = agentNamespace && modelNamespace !== agentNamespace;

            return (
              <SelectItem
                key={`${idx}_${model.ref}`}
                value={model.ref}
                disabled={!selectable}
                className={!selectable ? "opacity-50 cursor-not-allowed" : ""}
              >
                <div className="flex flex-col">
                  <span>{model.model} ({model.ref})</span>
                  {isDifferentNamespace && (
                    <span className="text-xs text-muted-foreground">
                      Change agent namespace to &quot;{modelNamespace}&quot; to use this model
                    </span>
                  )}
                </div>
              </SelectItem>
            );
          })}
        </SelectContent>
      </Select>
      {error && <p className="text-red-500 text-sm mt-1">{error}</p>}
      {allModels.length === 0 && <p className="text-amber-500 text-sm mt-1">No models available</p>}
    </>
  );
};
