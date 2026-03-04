import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ModelConfig } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface MemorySectionProps {
  allModels: ModelConfig[];
  selectedModel: Pick<ModelConfig, "ref" | "model"> | null;
  setSelectedModel: (model: Pick<ModelConfig, "ref" | "model"> | null) => void;
  agentNamespace?: string;
  ttlDays: string;
  onTtlChange: (value: string) => void;
  onTtlBlur?: () => void;
  modelError?: string;
  ttlError?: string;
  isSubmitting?: boolean;
}

export function MemorySection({
  allModels,
  selectedModel,
  setSelectedModel,
  agentNamespace,
  ttlDays,
  onTtlChange,
  onTtlBlur,
  modelError,
  ttlError,
  isSubmitting,
}: MemorySectionProps) {
  const getModelNamespace = (modelRef: string): string => {
    try {
      return k8sRefUtils.fromRef(modelRef).namespace;
    } catch {
      return "default";
    }
  };

  const isModelSelectable = (modelRef: string): boolean => {
    if (!agentNamespace) return true;
    const modelNamespace = getModelNamespace(modelRef);
    return modelNamespace === agentNamespace;
  };

  return (
    <div className="space-y-4">
      <div>
        <Label className="text-sm mb-2 block">Embedding Model</Label>
        <p className="text-xs mb-2 block text-muted-foreground">
          This model generates vector embeddings for memory. You can use a
          different provider than the LLM. Leave this empty to disbale memory.
        </p>
        <Select
          key={`memory-model-select-${agentNamespace}`}
          value={selectedModel?.ref || ""}
          disabled={isSubmitting || allModels.length === 0}
          onValueChange={(value) => {
            const model = allModels.find((m) => m.ref === value);
            if (model && isModelSelectable(model.ref)) {
              setSelectedModel(model);
            }
          }}
        >
          <SelectTrigger className={`${modelError ? "border-red-500" : ""}`}>
            <SelectValue placeholder="Select an embedding model" />
          </SelectTrigger>
          <SelectContent>
            {allModels.map((model, idx) => {
              const selectable = isModelSelectable(model.ref);
              const modelNamespace = getModelNamespace(model.ref);
              const isDifferentNamespace =
                agentNamespace && modelNamespace !== agentNamespace;

              return (
                <SelectItem
                  key={`${idx}_${model.ref}`}
                  value={model.ref}
                  disabled={!selectable}
                  className={!selectable ? "opacity-50 cursor-not-allowed" : ""}
                >
                  <div className="flex flex-col">
                    <span>
                      {model.model} ({model.ref})
                    </span>
                    {isDifferentNamespace && (
                      <span className="text-xs text-muted-foreground">
                        Change agent namespace to &quot;{modelNamespace}&quot;
                        to use this model
                      </span>
                    )}
                  </div>
                </SelectItem>
              );
            })}
          </SelectContent>
        </Select>
        {modelError && (
          <p className="text-red-500 text-sm mt-1">{modelError}</p>
        )}
        {allModels.length === 0 && (
          <p className="text-amber-500 text-sm mt-1">No models available</p>
        )}
      </div>

      <div>
        <Label className="text-sm mb-2 block">Memory TTL (days)</Label>
        <Input
          type="number"
          min={1}
          value={ttlDays}
          onChange={(e) => onTtlChange(e.target.value)}
          onBlur={onTtlBlur}
          placeholder="15"
          disabled={isSubmitting}
        />
        <p className="text-xs text-muted-foreground mt-2">
          Defaults to 15 days when left empty.
        </p>
        {ttlError && <p className="text-red-500 text-sm mt-1">{ttlError}</p>}
      </div>
    </div>
  );
}
