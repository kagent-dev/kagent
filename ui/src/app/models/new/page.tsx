"use client";
import React, { useState, useEffect } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Loader2, Eye, EyeOff, Pencil, ExternalLinkIcon } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { getModel, getSupportedProviders, createModelConfig, updateModelConfig } from "@/app/actions/models";
import { CreateModelConfigPayload, UpdateModelConfigPayload, Provider } from "@/lib/types";
import { toast } from "sonner";
import { isResourceNameValid } from "@/lib/utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import Link from "next/link";

interface ValidationErrors {
  name?: string;
  providerName?: string;
  model?: string;
  apiKey?: string;
  requiredParams?: Record<string, string>;
  optionalParams?: string;
}

interface ModelParam {
  id: string;
  key: string;
  value: string;
}

// Link mapping for models
const providerModelLinks: Record<string, string> = {
  "Anthropic": "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/anthropic/_model_info.py",
  "OpenAI": "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/openai/_model_info.py",
  "AzureOpenAI": "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/openai/_model_info.py",
  "Ollama": "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/ollama/_model_info.py",
};

// Link mapping for API Keys
const providerApiKeyLinks: Record<string, string> = {
  "OpenAI": "https://platform.openai.com/settings/api-keys",
  "AzureOpenAI": "https://ai.azure.com/", 
  "Anthropic": "https://console.anthropic.com/settings/keys",
};

function ModelPageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const isEditMode = searchParams.get("edit") === "true";
  const modelId = searchParams.get("id");

  const [name, setName] = useState("");
  const [isEditingName, setIsEditingName] = useState(false);
  const [selectedProvider, setSelectedProvider] = useState<Provider | null>(null);
  const [model, setModel] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [showApiKey, setShowApiKey] = useState(false);
  const [requiredParams, setRequiredParams] = useState<ModelParam[]>([]);
  const [optionalParams, setOptionalParams] = useState<ModelParam[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isLoading, setIsLoading] = useState(isEditMode);
  const [error, setError] = useState<string | null>(null);
  const [errors, setErrors] = useState<ValidationErrors>({});

  const isOllamaSelected = selectedProvider?.type === "Ollama";

  useEffect(() => {
    const fetchProviders = async () => {
      const response = await getSupportedProviders();
      if (response.success && response.data) {
        setProviders(response.data);
      }
    };
    fetchProviders();
  }, []);

  useEffect(() => {
    const fetchModelData = async () => {
      if (isEditMode && modelId && providers.length > 0) {
        try {
          setIsLoading(true);
          const response = await getModel(modelId);
          if (!response.success || !response.data) {
            throw new Error(response.error || "Failed to fetch model");
          }
          const modelData = response.data;
          setName(modelData.name);
          const provider = providers.find(p => p.type === modelData.providerName);
          setSelectedProvider(provider || null);
          setModel(modelData.model);
          setApiKey(""); // Don't fetch back API key

          const requiredKeys = provider?.requiredParams || [];
          const fetchedParams = modelData.modelParams || {};

          // 1. Build required params, handling null correctly
          const initialRequired: ModelParam[] = requiredKeys.map((key, index) => {
            const fetchedValue = fetchedParams[key];
            // Convert null/undefined to empty string, otherwise use String()
            const displayValue = (fetchedValue === null || fetchedValue === undefined) ? "" : String(fetchedValue);
            return {
              id: `req-${index}`,
              key: key,
              value: displayValue, 
            };
          });

          // 2. Build optional params, handling null correctly
          const initialOptional: ModelParam[] = Object.entries(fetchedParams)
            .filter(([key]) => !requiredKeys.includes(key))
            .map(([key, value], index) => {
              // Convert null/undefined to empty string, otherwise use String()
              const displayValue = (value === null || value === undefined) ? "" : String(value);
              return {
                id: `fetched-opt-${index}`,
                key,
                value: displayValue,
              };
            });

          console.log("Required Keys:", requiredKeys);
          console.log("Fetched Params:", fetchedParams);
          console.log("Initial Required:", initialRequired);
          console.log("Initial Optional:", initialOptional);

          setRequiredParams(initialRequired);
          setOptionalParams(initialOptional);

        } catch (err) {
          const errorMessage = err instanceof Error ? err.message : "Failed to fetch model";
          setError(errorMessage);
          toast.error(errorMessage);
        } finally {
          setIsLoading(false);
        }
      }
    };
    fetchModelData();
  }, [isEditMode, modelId, providers]);

  useEffect(() => {
    if (selectedProvider) {
      const requiredKeys = selectedProvider.requiredParams || [];
      const optionalKeys = selectedProvider.optionalParams || [];
      
      // If NOT in edit mode, initialize params from provider definition
      if (!isEditMode) {
          const newRequiredParams = requiredKeys.map((key, index) => ({
            id: `req-${index}`,
            key: key,
            value: "", // Start empty for create mode
          }));
          const newOptionalParams = optionalKeys.map((key, index) => ({
              id: `opt-${index}`,
              key: key,
              value: "", // Start empty for create mode
          }));
          setRequiredParams(newRequiredParams);
          setOptionalParams(newOptionalParams);
      } else {
        // In edit mode, the fetchModelData effect handles initialization.
        // This effect only needs to clear errors when provider changes during edit.
        setErrors(prev => ({ ...prev, requiredParams: {}, optionalParams: undefined }));
      }
    } else {
      setRequiredParams([]);
      setOptionalParams([]);
    }
  }, [selectedProvider, isEditMode]);

  useEffect(() => {
    if (!isEditMode && !isEditingName && selectedProvider && model) {
      const baseName = `${selectedProvider.type.toLowerCase()}-${model.toLowerCase()}`;
      const validName = baseName.replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '');
      if (isResourceNameValid(validName)) {
        setName(validName);
      }
    }
  }, [selectedProvider, model, isEditMode, isEditingName]);

  const validateForm = () => {
    const newErrors: ValidationErrors = { requiredParams: {} };

    if (!isResourceNameValid(name)) newErrors.name = "Name must be a valid RFC 1123 subdomain name";
    if (!selectedProvider) newErrors.providerName = "Provider is required";
    if (!model.trim()) newErrors.model = "Model is required";
    // API key is required only when creating AND provider is NOT Ollama
    if (!isEditMode && !isOllamaSelected && !apiKey.trim()) {
      newErrors.apiKey = "API key is required for new models (except Ollama)";
    }

    requiredParams.forEach(param => {
      if (!param.value.trim()) {
        if (!newErrors.requiredParams) newErrors.requiredParams = {};
        newErrors.requiredParams[param.key] = `${param.key} is required`;
      }
    });

    // Optional params don't need validation for being empty, but check for key uniqueness if needed
    const optionalKeys = new Set<string>();
    optionalParams.forEach(param => {
      if (param.key.trim()) {
        if (optionalKeys.has(param.key.trim())) {
            // This shouldn't happen if keys are pre-populated, but good check
            newErrors.optionalParams = "Duplicate parameter key detected"; 
        }
        optionalKeys.add(param.key.trim());
      }
    });

    setErrors(newErrors);
    const hasBaseErrors = !!newErrors.name || !!newErrors.providerName || !!newErrors.model || !!newErrors.apiKey;
    const hasRequiredParamErrors = Object.keys(newErrors.requiredParams || {}).length > 0;
    const hasOptionalParamErrors = !!newErrors.optionalParams;
    return !hasBaseErrors && !hasRequiredParamErrors && !hasOptionalParamErrors;
  };

  const handleRequiredParamChange = (index: number, value: string) => {
    const newParams = [...requiredParams];
    newParams[index].value = value;
    setRequiredParams(newParams);
    if (errors.requiredParams && errors.requiredParams[newParams[index].key]) {
      const updatedParamErrors = { ...errors.requiredParams };
      delete updatedParamErrors[newParams[index].key];
      setErrors(prev => ({ ...prev, requiredParams: updatedParamErrors }));
    }
  };

  const handleOptionalParamChange = (index: number, value: string) => {
    const newParams = [...optionalParams];
    newParams[index].value = value;
    setOptionalParams(newParams);
    if (errors.optionalParams) {
      setErrors(prev => ({ ...prev, optionalParams: undefined }));
    }
  };

  const handleSubmit = async () => {
    if (!validateForm() || !selectedProvider) {
      toast.error("Please fill in all required fields and correct any errors.");
      return;
    }
    setIsSubmitting(true);
    setErrors({});

    // Combine required and optional params into a single map for backend
    const allParams = [...requiredParams, ...optionalParams]
      .filter(p => p.key.trim() !== "" && p.value.trim() !== "") // Filter out empty keys or values
      .reduce((acc, param) => {
        acc[param.key.trim()] = param.value.trim();
        return acc;
      }, {} as Record<string, string>);

    // Construct the payload based on the selected provider
    const payload: CreateModelConfigPayload = {
      name: name.trim(),
      provider: {
        name: selectedProvider.name,
        type: selectedProvider.type,
      },
      model: model.trim(),
      apiKey: apiKey.trim(),
    };

    // Add provider-specific params under the correct key
    const providerType = selectedProvider.type;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const providerParams: Record<string, any> = {};

    // Define known numeric keys based on Go structs
    const numericKeys = new Set([
        'maxTokens',
        'topK',
        'seed',
        'n',
        'timeout'
    ]);

    // Populate providerParams from allParams based on the expected structure
    Object.entries(allParams).forEach(([key, value]) => {
      if (numericKeys.has(key)) {
        const numValue = parseInt(value, 10);
        if (!isNaN(numValue)) {
          providerParams[key] = numValue;
        } else {
          console.warn(`Invalid number for parameter '${key}': '${value}'. Omitting.`);
        }
      } else {
        providerParams[key] = value;
      }
    });

    if (providerType === 'OpenAI') {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      payload.openAI = providerParams as any;
    } else if (providerType === 'Anthropic') {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      payload.anthropic = providerParams as any;
    } else if (providerType === 'AzureOpenAI') {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      payload.azureOpenAI = providerParams as any;
    } else if (providerType === 'Ollama') {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      payload.ollama = providerParams as any;
    }

    try {
      let response;
      if (isEditMode && modelId) {
        const updatePayload: UpdateModelConfigPayload = {
          ...payload,
          apiKey: apiKey.trim() ? apiKey.trim() : null, // Set to null if empty for update
        };
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        delete (updatePayload as any).name; // Name cannot be updated
        response = await updateModelConfig(modelId, updatePayload);
      } else {
        response = await createModelConfig(payload);
      }

      if (response.success) {
        toast.success(`Model configuration ${isEditMode ? 'updated' : 'created'} successfully!`);
        router.push("/models");
      } else {
        throw new Error(response.error || "Failed to save model configuration");
      }
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "An unexpected error occurred";
      console.error("Submission error:", err);
      setError(errorMessage);
      toast.error(errorMessage);
    } finally {
      setIsSubmitting(false);
    }
  };

  if (error) {
    return <ErrorState message={error} />;
  }

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-6xl mx-auto">
        <h1 className="text-2xl font-bold mb-8">{isEditMode ? "Edit Model" : "Create New Model"}</h1>

        <div className="space-y-6">
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
                      onChange={(e) => setName(e.target.value)}
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
                    onClick={() => setIsEditingName(!isEditingName)}
                    title={isEditingName ? "Finish Editing Name" : "Edit Auto-Generated Name"}
                    disabled={isSubmitting || isLoading}
                  >
                    <Pencil className="h-4 w-4" />
                  </Button>
                </div>
                {errors.name && <p className="text-destructive text-sm mt-1">{errors.name}</p>}
              </div>

              <div>
                <label className="text-sm mb-2 block">Provider</label>
                <Select
                  value={selectedProvider?.type || ""}
                  onValueChange={(value) => {
                    const provider = providers.find(p => p.type === value);
                    setSelectedProvider(provider || null);
                  }}
                  disabled={isSubmitting || isLoading}
                >
                  <SelectTrigger className={errors.providerName ? "border-destructive" : ""}>
                    <SelectValue placeholder="Select a provider">
                      {selectedProvider?.name || "Select a provider"}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {providers.map((provider) => (
                      <SelectItem key={provider.type} value={provider.type}>
                        {provider.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {errors.providerName && <p className="text-destructive text-sm mt-1">{errors.providerName}</p>}
              </div>

              <div>
                <label htmlFor="model-input" className="text-sm mb-2 block">Model Name</label>
                <div className="flex items-center space-x-2">
                  <Input
                    id="model-input"
                    value={model}
                    onChange={(e) => setModel(e.target.value)}
                    className={`${errors.model ? "border-destructive" : ""} flex-grow`}
                    placeholder="Enter model name (e.g., gpt-4, claude-3-opus-20240229)"
                    disabled={isSubmitting || isLoading}
                  />
                  {selectedProvider && providerModelLinks[selectedProvider.type] && (
                    <Button variant="outline" size="icon" asChild>
                      <Link href={providerModelLinks[selectedProvider.type]} target="_blank" rel="noopener noreferrer" title={`View available ${selectedProvider.name} models`}>
                        <ExternalLinkIcon className="h-4 w-4" />
                      </Link>
                    </Button>
                  )}
                </div>
                {errors.model && <p className="text-destructive text-sm mt-1">{errors.model}</p>}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Authentication</CardTitle>
            </CardHeader>
            <CardContent>
              {!isOllamaSelected ? (
                <div>
                  <label className="text-sm mb-2 block">
                    API Key {isEditMode && "(Leave blank to keep existing)"}
                  </label>
                  <div className="flex items-center space-x-2">
                    <div className="relative flex-grow">
                       <Input
                         type={showApiKey ? "text" : "password"}
                         value={apiKey}
                         onChange={(e) => setApiKey(e.target.value)}
                         className={`${errors.apiKey ? "border-destructive" : ""} pr-10 w-full`}
                         placeholder={isEditMode ? "Enter new API key to update" : "Enter API key..."}
                         disabled={isSubmitting || isLoading}
                         autoComplete="new-password"
                       />
                       <Button
                         type="button"
                         variant="ghost"
                         size="sm"
                         className="absolute right-0 top-0 h-full px-3"
                         onClick={() => setShowApiKey(!showApiKey)}
                         disabled={isSubmitting || isLoading} 
                         title={showApiKey ? "Hide API Key" : "Show API Key"}
                       >
                         {showApiKey ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                       </Button>
                     </div>
                     {selectedProvider && providerApiKeyLinks[selectedProvider.type] && (
                        <Button variant="outline" size="icon" asChild>
                          <Link href={providerApiKeyLinks[selectedProvider.type]} target="_blank" rel="noopener noreferrer" title={`Find your ${selectedProvider.name} API key`}>
                            <ExternalLinkIcon className="h-4 w-4" />
                           </Link>
                         </Button>
                     )}
                   </div>
                   {errors.apiKey && <p className="text-destructive text-sm mt-1">{errors.apiKey}</p>}
                 </div>
              ) : (
                <div className="border bg-accent border-border p-3 rounded text-sm text-accent-foreground">
                  Ollama models run locally and do not require an API key.
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Parameters</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              {selectedProvider && requiredParams.length > 0 && (
                <div>
                  <label className="text-sm font-medium mb-2 block text-gray-800">Required</label>
                  <div className="space-y-3 pl-4 border-l-2 border-border">
                    {requiredParams.map((param, index) => (
                      <div key={param.key} className="space-y-1">
                        <label htmlFor={`required-param-${param.key}`} className="text-xs font-medium text-gray-700">{param.key}</label>
                        <Input
                          id={`required-param-${param.key}`}
                          placeholder={`Enter value for ${param.key}`}
                          value={param.value}
                          onChange={(e) => handleRequiredParamChange(index, e.target.value)}
                          className={errors.requiredParams?.[param.key] ? "border-destructive" : ""}
                          disabled={isSubmitting || isLoading}
                        />
                        {errors.requiredParams?.[param.key] && <p className="text-destructive text-sm mt-1">{errors.requiredParams[param.key]}</p>}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {selectedProvider && optionalParams.length > 0 && (
                <div>
                  <label className="text-sm font-medium mb-2 block text-gray-800">Optional</label>
                  <div className="space-y-3 pl-4 border-l-2 border-border">
                    {optionalParams.map((param, index) => (
                      <div key={param.key} className="space-y-1">
                        <label htmlFor={`optional-param-${param.key}`} className="text-xs font-medium text-gray-700">{param.key}</label>
                        <Input
                          id={`optional-param-${param.key}`}
                          placeholder={`(Optional) Enter value for ${param.key}`}
                          value={param.value}
                          onChange={(e) => handleOptionalParamChange(index, e.target.value)}
                          disabled={isSubmitting || isLoading}
                        />
                      </div>
                    ))}
                    {errors.optionalParams && <p className="text-destructive text-sm mt-1">{errors.optionalParams}</p>}
                  </div>
                </div>
              )}

              {!selectedProvider && (
                <div className="text-sm text-muted-foreground">
                    Select a provider to view and configure its parameters.
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        <div className="flex justify-end pt-6">
          <Button
            variant="default"
            onClick={handleSubmit}
            disabled={isSubmitting || isLoading}
          >
            {isSubmitting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                {isEditMode ? "Updating..." : "Creating..."}
              </>
            ) : isEditMode ? (
              "Update Model"
            ) : (
              "Create Model"
            )}
          </Button>
        </div>

      </div>
    </div>
  );
}

export default function ModelPage() {
  return (
    <React.Suspense fallback={<LoadingState />}>
      <ModelPageContent />
    </React.Suspense>
  );
} 