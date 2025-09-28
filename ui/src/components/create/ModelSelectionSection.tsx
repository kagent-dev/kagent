import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { useEffect, useState } from "react";
import type { ModelConfig } from "@/types";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface ModelSelectionSectionProps {
  allModels: ModelConfig[];
  selectedModel: Partial<ModelConfig> | null;
  setSelectedModel: (model: Partial<ModelConfig> | null) => void;
  // Optional multi-select support
  multi?: boolean;
  selectedModels?: Partial<ModelConfig>[];
  setSelectedModels?: (models: Partial<ModelConfig>[]) => void;
  error?: string;
  isSubmitting: boolean;
  onChange?: (modelRef: string) => void;
  agentNamespace?: string;
}

export const ModelSelectionSection = ({
  allModels,
  selectedModel,
  setSelectedModel,
  multi = false,
  selectedModels = [],
  setSelectedModels,
  error,
  isSubmitting,
  onChange,
  agentNamespace
}: ModelSelectionSectionProps) => {
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});

  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch('/api/admin/models-settings', { headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` } });
        if (res.ok) {
          const data = await res.json();
          setEnabledMap(data.enabled || {});
        }
      } catch {
        // ignore; default all enabled
      }
    };
    load();
  }, []);
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
    const enabled = Object.prototype.hasOwnProperty.call(enabledMap, modelRef) ? enabledMap[modelRef] : true;
    return modelNamespace === agentNamespace && enabled;
  };

  if (!multi) {
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
  }

  // Multi-select mode
  const toggleModel = (ref: string) => {
    if (!setSelectedModels) return;
    const model = allModels.find((m) => m.ref === ref);
    if (!model || !isModelSelectable(model.ref)) return;
    const exists = selectedModels.some((m) => m.ref === ref);
    if (exists) {
      setSelectedModels(selectedModels.filter((m) => m.ref !== ref));
    } else {
      setSelectedModels([...selectedModels, model]);
    }
    onChange?.(ref);
  };

  return (
    <>
      <label className="text-base mb-2 block font-bold">Models</label>
      <p className="text-xs mb-3 block text-muted-foreground">
        Select one or more models to run your agent. The first selected will be used as the primary.
        {agentNamespace && (
          <span className="block mt-1">
            Only models from the <strong>{agentNamespace}</strong> namespace are selectable.
          </span>
        )}
      </p>
      <div className="space-y-2 max-h-64 overflow-auto border rounded-md p-2">
        {allModels.map((model, idx) => {
          const selectable = isModelSelectable(model.ref);
          const checked = selectedModels.some((m) => m.ref === model.ref);
          return (
            <div key={`${idx}_${model.ref}`} className={`flex items-center gap-2 ${!selectable ? "opacity-50" : ""}`}>
              <Checkbox
                id={`model-${idx}`}
                checked={checked}
                onCheckedChange={() => toggleModel(model.ref)}
                disabled={!selectable || isSubmitting}
              />
              <label htmlFor={`model-${idx}`} className="text-sm cursor-pointer">
                {model.model} ({model.ref})
              </label>
            </div>
          );
        })}
      </div>
      {error && <p className="text-red-500 text-sm mt-1">{error}</p>}
      {allModels.length === 0 && <p className="text-amber-500 text-sm mt-1">No models available</p>}
    </>
  );
};
