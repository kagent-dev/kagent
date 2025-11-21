"use client";

import * as React from "react";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Loader2 } from "lucide-react";
import { useAgents } from "@/components/AgentsProvider";
import { k8sRefUtils } from "@/lib/k8sUtils";
import type { LLMNodeData } from "@/types";

interface LLMPropertyEditorProps {
  data: LLMNodeData;
  onUpdate: (data: Record<string, unknown>) => void;
  agentNamespace?: string;
}

export function LLMPropertyEditor({ data, onUpdate, agentNamespace }: LLMPropertyEditorProps) {
  const nodeData = data as LLMPropertyEditorProps['data'];
  const { models, loading } = useAgents();

  const handleModelSelect = (modelRef: string) => {
    const selectedModel = models.find(m => m.ref === modelRef);
    if (selectedModel && isModelSelectable(selectedModel.ref)) {
      // Extract provider from model name
      const provider = extractProvider(selectedModel.model);
      
      onUpdate({
        ...nodeData,
        modelConfigRef: selectedModel.ref,
        modelName: selectedModel.model,
        provider: provider,
      });
    }
  };

  const extractProvider = (modelName: string): string => {
    if (!modelName) return 'OpenAI';
    
    const lowerModel = modelName.toLowerCase();
    if (lowerModel.startsWith('gpt-') || lowerModel.startsWith('o1-')) {
      return 'OpenAI';
    } else if (lowerModel.startsWith('claude-')) {
      return 'Anthropic';
    } else if (lowerModel.includes('gemini')) {
      return 'Gemini';
    } else if (lowerModel.includes('llama') || lowerModel.includes('mistral')) {
      return 'Ollama';
    }
    return 'OpenAI';
  };

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
    <div className="space-y-4">
      <div>
        <Label htmlFor="model" className="text-xs mb-2 block">Model</Label>
        {loading ? (
          <div className="flex items-center justify-center py-2 text-muted-foreground">
            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            <span className="text-xs">Loading models...</span>
          </div>
        ) : (
          <>
            <Select
              key={`model-select-${agentNamespace}`}
              value={nodeData.modelConfigRef || ""}
              onValueChange={handleModelSelect}
              disabled={models.length === 0}
            >
              <SelectTrigger className="text-sm h-9">
                <SelectValue placeholder="Select a model" />
              </SelectTrigger>
              <SelectContent>
                {models.map((model, idx) => {
                  const modelNamespace = getModelNamespace(model.ref);
                  const selectable = isModelSelectable(model.ref);
                  const isDifferentNamespace = agentNamespace && modelNamespace !== agentNamespace;

                  return (
                    <SelectItem
                      key={`${idx}_${model.ref}`}
                      value={model.ref}
                      disabled={!selectable}
                      className={!selectable ? "opacity-50 cursor-not-allowed" : ""}
                    >
                      <div className="flex flex-col">
                        <span className="text-sm">{model.model}</span>
                        <span className="text-xs text-muted-foreground">
                          {modelNamespace}/{model.ref.split('/')[1]}
                        </span>
                        {isDifferentNamespace && (
                          <span className="text-xs text-amber-600 dark:text-amber-400">
                            Change agent namespace to &quot;{modelNamespace}&quot; to use this model
                          </span>
                        )}
                      </div>
                    </SelectItem>
                  );
                })}
              </SelectContent>
            </Select>
            {models.length === 0 && (
              <p className="text-amber-500 text-xs mt-1">
                No models available. Please create a model first.
              </p>
            )}
          </>
        )}
        <p className="text-xs text-muted-foreground mt-1">
          {agentNamespace 
            ? `Only models from the ${agentNamespace} namespace are selectable.`
            : 'Select the LLM model for this agent'
          }
        </p>
      </div>

      {nodeData.provider && (
        <div>
          <Label className="text-xs mb-1 block">Provider</Label>
          <div className="text-sm py-2 px-3 bg-muted rounded-md">
            {nodeData.provider}
          </div>
          <p className="text-xs text-muted-foreground mt-1">
            Auto-detected from model selection
          </p>
        </div>
      )}
    </div>
  );
}
