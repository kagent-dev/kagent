import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { ModelConfig } from "@/lib/types";

interface ModelSelectionSectionProps {
  allModels: ModelConfig[];
  selectedModel: Partial<ModelConfig> | null;
  setSelectedModel: (model: Partial<ModelConfig>) => void;
  error?: string;
  isSubmitting: boolean;
  onBlur?: () => void;
}

export const ModelSelectionSection = ({ allModels, selectedModel, setSelectedModel, error, isSubmitting, onBlur }: ModelSelectionSectionProps) => {
  return (
    <>
      <label className="text-base mb-2 block font-bold">Model</label>
      <p className="text-xs mb-2 block text-muted-foreground">
        This is the model that will be used to generate the agent&apos;s responses.
      </p>
      <Select 
        value={selectedModel?.ref || ""} 
        disabled={isSubmitting || allModels.length === 0} 
        onValueChange={(value) => {
          const model = allModels.find((m) => m.ref === value);
          if (model) {
            setSelectedModel(model);
            if (onBlur) {
              onBlur();
            }
          }
        }}
      >
        <SelectTrigger className={`${error ? "border-red-500" : ""}`}>
          <SelectValue placeholder="Select a model" />
        </SelectTrigger>
        <SelectContent>
          {allModels.map((model, idx) => (
            <SelectItem key={`${idx}_${model.ref}`} value={model.ref}>
              {model.model} ({model.ref})
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {error && <p className="text-red-500 text-sm mt-1">{error}</p>}
      {allModels.length === 0 && <p className="text-amber-500 text-sm mt-1">No models available</p>}
    </>
  );
};
