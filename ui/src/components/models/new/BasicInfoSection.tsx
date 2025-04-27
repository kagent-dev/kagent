import React from 'react';
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Pencil, ExternalLinkIcon, AlertCircle } from "lucide-react";
import Link from "next/link";
import { Provider } from "@/lib/types";
import { ProviderModelsResponse } from '@/app/actions/models';
import { ModelProviderCombobox } from "@/components/ModelProviderCombobox";
import { PROVIDERS_INFO, getProviderFormKey, ModelProviderKey, BackendModelProviderType } from "@/lib/providers";

interface ValidationErrors {
  name?: string;
  selectedCombinedModel?: string;
  apiKey?: string;
  requiredParams?: Record<string, string>;
  optionalParams?: string;
}

interface BasicInfoSectionProps {
  name: string;
  isEditingName: boolean;
  errors: ValidationErrors;
  isSubmitting: boolean;
  isLoading: boolean;
  onNameChange: (value: string) => void;
  onToggleEditName: () => void;
  providers: Provider[];
  providerModelsData: ProviderModelsResponse | null;
  selectedCombinedModel: string | undefined;
  onModelChange: (comboboxValue: string, providerKey: ModelProviderKey, modelName: string, functionCalling: boolean) => void;
  selectedProvider: Provider | null;
  selectedModelSupportsFunctionCalling: boolean | null;
  loadingError: string | null;
  isEditMode: boolean;
}

export const BasicInfoSection: React.FC<BasicInfoSectionProps> = ({
  name, isEditingName, errors, isSubmitting, isLoading, onNameChange,
  onToggleEditName, providers, providerModelsData, selectedCombinedModel,
  onModelChange, selectedProvider, selectedModelSupportsFunctionCalling,
  loadingError, isEditMode
}) => {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Basic Information</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <label className="text-sm mb-2 block">Name</label>
          <div className="flex items-center space-x-2">
            {isEditingName ? (
              <Input
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                className={errors.name ? "border-destructive" : ""}
                placeholder="Enter model name..."
                disabled={isSubmitting || isLoading}
              />
            ) : (
              <div className={`flex-1 py-2 px-3 border rounded-md bg-muted ${errors.name ? 'border-destructive' : 'border-input'}`}>
                {name || "(Name will be auto-generated)"}
              </div>
            )}
            <Button
              variant="outline"
              size="icon"
              onClick={onToggleEditName}
              title={isEditingName ? "Finish Editing Name" : "Edit Auto-Generated Name"}
              disabled={isSubmitting || isLoading}
            >
              <Pencil className="h-4 w-4" />
            </Button>
          </div>
          {errors.name && <p className="text-destructive text-sm mt-1">{errors.name}</p>}
        </div>

        <div>
          <label className="text-sm mb-2 block">Provider & Model</label>
          <div className="flex items-center space-x-2">
            <div className="flex-grow">
              <ModelProviderCombobox
                providers={providers}
                models={providerModelsData}
                value={selectedCombinedModel}
                onChange={onModelChange}
                disabled={isSubmitting || isLoading || isEditMode}
                loading={isLoading}
                error={loadingError}
                filterFunctionCalling={false}
                placeholder="Select Provider & Model..."
              />
            </div>
            {selectedProvider && (
              (() => {
                 const providerKey = getProviderFormKey(selectedProvider.type as BackendModelProviderType);
                 const providerInfo = providerKey ? PROVIDERS_INFO[providerKey] : undefined;
                 return providerInfo?.modelDocsLink ? (
                   <Button variant="outline" size="icon" asChild>
                     <Link href={providerInfo.modelDocsLink} target="_blank" rel="noopener noreferrer" title={`View available ${selectedProvider.name} models`}>
                       <ExternalLinkIcon className="h-4 w-4" />
                     </Link>
                   </Button>
                 ) : null;
               })()
            )}
          </div>
          {errors.selectedCombinedModel && <p className="text-destructive text-sm mt-1">{errors.selectedCombinedModel}</p>}
          {selectedCombinedModel && selectedModelSupportsFunctionCalling === false && (
            <p className="text-[0.8rem] text-yellow-600 flex items-center gap-1 mt-2">
              <AlertCircle className="h-4 w-4 flex-shrink-0" />
              Note: This model does not support function calling.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}; 