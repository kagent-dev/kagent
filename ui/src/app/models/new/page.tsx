"use client";
import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { getModelConfig, createModelConfig, updateModelConfig } from "@/app/actions/modelConfigs";
import { useAgents } from "@/components/AgentsProvider";
import type {
    CreateModelConfigRequest,
    UpdateModelConfigPayload,
    Provider,
    OpenAIConfigPayload,
    AzureOpenAIConfigPayload,
    AnthropicConfigPayload,
    OllamaConfigPayload,
    ProviderModelsResponse,
    GeminiConfigPayload,
    GeminiVertexAIConfigPayload,
    AnthropicVertexAIConfigPayload
} from "@/types";
import { toast } from "sonner";
import { isResourceNameValid, createRFC1123ValidName } from "@/lib/utils";
import { OLLAMA_DEFAULT_TAG } from "@/lib/constants"
import { getSupportedModelProviders, getConfiguredProviders, getConfiguredProviderModels } from "@/app/actions/providers";
import { getModels } from "@/app/actions/models";
import { isValidProviderInfoKey, getProviderFormKey, ModelProviderKey, BackendModelProviderType } from "@/lib/providers";
import { BasicInfoSection } from '@/components/models/new/BasicInfoSection';
import { AuthSection } from '@/components/models/new/AuthSection';
import { ParamsSection } from '@/components/models/new/ParamsSection';
import { k8sRefUtils } from "@/lib/k8sUtils";
import { useReadOnly } from "@/components/ReadOnlyProvider";

interface ValidationErrors {
  name?: string;
  namespace?: string;
  selectedCombinedModel?: string;
  apiKey?: string;
  requiredParams?: Record<string, string>;
  optionalParams?: string;
}

interface ModelParam {
  id: string;
  key: string;
  value: string;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const processModelParams = (requiredParams: ModelParam[], optionalParams: ModelParam[]): Record<string, any> => {
  const allParams = [...requiredParams, ...optionalParams]
    .filter(p => p.key.trim() !== "")
    .reduce((acc, param) => {
      acc[param.key.trim()] = param.value;
      return acc;
    }, {} as Record<string, string>);

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const providerParams: Record<string, any> = {};
  const numericKeys = new Set([
    'maxTokens',
    'topK',
    'seed',
    'n',
    'timeout',
  ]);

  const booleanKeys = new Set([
    'stream'
  ]);

  Object.entries(allParams).forEach(([key, value]) => {
    if (numericKeys.has(key)) {
      const numValue = parseFloat(value);
      if (!isNaN(numValue)) {
        providerParams[key] = numValue;
      } else {
        if (value.trim() !== '') {
          console.warn(`Invalid number for parameter '${key}': '${value}'. Treating as unset.`);
        }
      }
    } else if (booleanKeys.has(key)) {
      const lowerValue = value.toLowerCase().trim();
      if (lowerValue === 'true' || lowerValue === '1' || lowerValue === 'yes') {
        providerParams[key] = true;
      } else if (lowerValue === 'false' || lowerValue === '0' || lowerValue === 'no' || lowerValue === '') {
        providerParams[key] = false;
      } else {
        console.warn(`Invalid boolean for parameter '${key}': '${value}'. Treating as false.`);
        providerParams[key] = false;
      }
    } else {
      if (value.trim() !== '') {
        providerParams[key] = value;
      }
    }
  });

  return providerParams;
}

function ModelPageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { refreshModels } = useAgents();

  const isEditMode = searchParams.get("edit") === "true";
  const modelConfigName = searchParams.get("name");
  const modelConfigNamespace = searchParams.get("namespace");

  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("");
  const [isEditingName, setIsEditingName] = useState(false);
  const [selectedProvider, setSelectedProvider] = useState<Provider | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [showApiKey, setShowApiKey] = useState(false);
  const [requiredParams, setRequiredParams] = useState<ModelParam[]>([]);
  const [optionalParams, setOptionalParams] = useState<ModelParam[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [providerModelsData, setProviderModelsData] = useState<ProviderModelsResponse | null>(null);
  const [selectedCombinedModel, setSelectedCombinedModel] = useState<string | undefined>(undefined);
  const [selectedModelSupportsFunctionCalling, setSelectedModelSupportsFunctionCalling] = useState<boolean | null>(null);
  const [modelTag, setModelTag] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [loadingError, setLoadingError] = useState<string | null>(null);
  const [errors, setErrors] = useState<ValidationErrors>({});
  const [isApiKeyNeeded, setIsApiKeyNeeded] = useState(true);
  const [isParamsSectionExpanded, setIsParamsSectionExpanded] = useState(false);
  const [isFetchingModels, setIsFetchingModels] = useState(false);
  const isOllamaSelected = selectedProvider?.type === "Ollama";

  useEffect(() => {
    let isMounted = true;
    const fetchData = async () => {
      setLoadingError(null);
      setIsLoading(true);
      try {
        const [stockProvidersResponse, configuredProvidersResponse, modelsResponse] = await Promise.all([
          getSupportedModelProviders(),
          getConfiguredProviders(),
          getModels()
        ]);

        if (!isMounted) return;

        // Merge stock and configured providers
        const stockProviders: Provider[] = (stockProvidersResponse.data || []).map(p => ({
          ...p,
          source: 'stock' as const
        }));

        const configuredProviders: Provider[] = (configuredProvidersResponse.data || []).map(cp => {
          // Find the stock provider with the same type to get its params
          const stockProvider = stockProviders.find(sp => sp.type === cp.type);
          return {
            name: cp.name,
            type: cp.type,
            requiredParams: stockProvider?.requiredParams || [],
            optionalParams: stockProvider?.optionalParams || [],
            source: 'configured' as const,
            endpoint: cp.endpoint
          };
        });

        const allProviders = [...stockProviders, ...configuredProviders];
        setProviders(allProviders);

        if (!modelsResponse.error && modelsResponse.data) {
          setProviderModelsData(modelsResponse.data);
        } else {
          throw new Error(modelsResponse.error || "Failed to fetch available models");
        }
      } catch (err) {
        console.error("Error fetching initial data:", err);
        const message = err instanceof Error ? err.message : "Failed to load providers or models";
        if (isMounted) {
          setLoadingError(message);
          setError(message);
        }
      } finally {
        if (isMounted) {
          if (!isEditMode) {
            setIsLoading(false);
          }
        }
      }
    };
    fetchData();
    return () => { isMounted = false; };
  }, [isEditMode]);

  useEffect(() => {
    let isMounted = true;
    const fetchModelData = async () => {
      if (isEditMode && modelConfigName && providers.length > 0 && providerModelsData) {
        try {
          setIsLoading(true);
          const response = await getModelConfig(
            k8sRefUtils.toRef(modelConfigNamespace || '', modelConfigName)
          );
          if (!isMounted) return;

          if (response.error || !response.data) {
            throw new Error(response.error || "Failed to fetch model");
          }
          const modelData = response.data;
          const modelRef = k8sRefUtils.fromRef(modelData.ref);
          setName(modelRef.name);
          setNamespace(modelRef.namespace);

          const provider = providers.find(p => p.type === modelData.providerName);
          setSelectedProvider(provider || null);

          setApiKey("");

          const providerFormKey = provider ? getProviderFormKey(provider.type as BackendModelProviderType) : undefined;
          let modelName = modelData.model;
          let extractedTag;

          if (modelData.providerName === 'Ollama' && modelName.includes(':')) {
            const [name, tag] = modelName.split(':');
            modelName = name;
            extractedTag = tag;
          }

          if (providerFormKey && modelData.model) {
            setSelectedCombinedModel(`${providerFormKey}::${modelName}`);
          }

          if (!modelData.apiKeySecretRef) {
            setIsApiKeyNeeded(false);
          } else {
            setIsApiKeyNeeded(true);
          }

          const fetchedParams = modelData.modelParams || {};
          if (provider?.type === 'Ollama') {
            setModelTag(fetchedParams.modelTag || extractedTag || 'latest');
          }

          const requiredKeys = provider?.requiredParams || [];
          const initialRequired: ModelParam[] = requiredKeys.map((key, index) => {
            const fetchedValue = fetchedParams[key];
            const displayValue = (fetchedValue === null || fetchedValue === undefined) ? "" : String(fetchedValue);
            return { id: `req-${index}`, key: key, value: displayValue };
          });

          const initialOptional: ModelParam[] = Object.entries(fetchedParams)
            .filter(([key]) => !requiredKeys.includes(key))
            .map(([key, value], index) => {
              const displayValue = (value === null || value === undefined) ? "" : String(value);
              return { id: `fetched-opt-${index}`, key, value: displayValue };
            });

            setRequiredParams(initialRequired);
            setOptionalParams(initialOptional);

        } catch (err) {
          const errorMessage = err instanceof Error ? err.message : "Failed to fetch model";
          if (isMounted) {
            setError(errorMessage);
            setLoadingError(errorMessage);
            toast.error(errorMessage);
          }
        } finally {
          if (isMounted) {
            setIsLoading(false);
          }
        }
      }
    };
    fetchModelData();
    return () => { isMounted = false; };
  }, [isEditMode, modelConfigName, providers, providerModelsData, modelConfigNamespace]);

  // Auto-fetch models when provider is selected and models are not available
  useEffect(() => {
    let isMounted = true;
    const fetchProviderModels = async () => {
      if (!selectedProvider || isEditMode) return;

      const providerKey = getProviderFormKey(selectedProvider.type as BackendModelProviderType);
      if (!providerKey) return;

      // Check if models are already available for this provider
      const hasModels = providerModelsData?.[providerKey] && providerModelsData[providerKey].length > 0;
      if (hasModels) return;

      try {
        if (selectedProvider.source === 'configured') {
          // Fetch models for configured provider
          const response = await getConfiguredProviderModels(selectedProvider.name, false);

          if (!isMounted) return;

          if (response.error || !response.data) {
            console.error(`Failed to fetch models for ${selectedProvider.name}:`, response.error);
            return;
          }

          const models = response.data.models.map(modelName => ({
            name: modelName,
            function_calling: true
          }));

          setProviderModelsData(prev => ({
            ...prev,
            [providerKey]: models
          }));
        } else {
          // Fetch all stock models if stock provider is selected and models are missing
          const response = await getModels();

          if (!isMounted) return;

          if (response.error || !response.data) {
            console.error('Failed to fetch stock models:', response.error);
            return;
          }

          setProviderModelsData(response.data);
        }
      } catch (error) {
        console.error('Error fetching provider models:', error);
      }
    };

    fetchProviderModels();
    return () => { isMounted = false; };
  }, [selectedProvider, isEditMode, providerModelsData]);

  useEffect(() => {
    if (selectedProvider) {
      const requiredKeys = selectedProvider.requiredParams || [];
      let optionalKeys = [...(selectedProvider.optionalParams || [])];

      // Add baseUrl to optional params for providers that support it
      const providersWithBaseUrl = ['OpenAI', 'Anthropic', 'Gemini'];
      if (providersWithBaseUrl.includes(selectedProvider.type) && !optionalKeys.includes('baseUrl')) {
        optionalKeys = ['baseUrl', ...optionalKeys];
      }

      const currentModelRequiresReset = !isEditMode;

      if (currentModelRequiresReset) {
        const newRequiredParams = requiredKeys.map((key, index) => ({
          id: `req-${index}`,
          key: key,
          value: "",
        }));
        const newOptionalParams = optionalKeys.map((key, index) => ({
          id: `opt-${index}`,
          key: key,
          value: "",
        }));
        setRequiredParams(newRequiredParams);
        setOptionalParams(newOptionalParams);
      }

      setErrors(prev => ({ ...prev, requiredParams: {}, optionalParams: undefined }));

    } else {
      setRequiredParams([]);
      setOptionalParams([]);
    }
  }, [selectedProvider, isEditMode]);

  useEffect(() => {
    if (!isEditMode && !isEditingName && selectedCombinedModel) {
      const parts = selectedCombinedModel.split('::');
      if (parts.length === 2) {
        const providerKey = parts[0];
        const modelName = parts[1];
        const nameParts = [providerKey, modelName];

        const isOllama = selectedProvider?.type === "Ollama";
        if (isOllama && modelTag && modelTag !== OLLAMA_DEFAULT_TAG) {
          nameParts.push(modelTag);
        }

        const validName = createRFC1123ValidName(nameParts);
        if (validName && isResourceNameValid(validName)) {
          setName(validName);
        }
      }
    }
  }, [selectedCombinedModel, isEditMode, isEditingName, modelTag, selectedProvider]);

  useEffect(() => {
    if (!isApiKeyNeeded) {
      setApiKey("");
      if (errors.apiKey) {
        setErrors(prev => ({ ...prev, apiKey: undefined }));
      }
    }
  }, [isApiKeyNeeded, errors.apiKey]);

  // Auto-select provider on page load (create mode only)
  // Default: select stock OpenAI provider
  useEffect(() => {
    if (!isEditMode && providers.length > 0 && !selectedProvider) {
      const openAIProvider = providers.find(p => p.type === 'OpenAI' && p.source === 'stock');
      if (openAIProvider) {
        setSelectedProvider(openAIProvider);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isEditMode]); // Only run when isEditMode changes (initial mount)

  const validateForm = () => {
    const newErrors: ValidationErrors = { requiredParams: {} };

    if (!isResourceNameValid(name)) newErrors.name = "Name must be a valid RFC 1123 subdomain name";
    if (!selectedCombinedModel) newErrors.selectedCombinedModel = "Provider and Model selection is required";
    const isOllamaNow = selectedCombinedModel?.startsWith('ollama::');
    if (!isEditMode && !isOllamaNow && isApiKeyNeeded && !apiKey.trim()) {
      newErrors.apiKey = "API key is required for new models (except for Ollama or when you don't need an API key)";
    }

    requiredParams.forEach(param => {
      if (!param.value.trim() && param.key.trim()) {
        if (!newErrors.requiredParams) newErrors.requiredParams = {};
        newErrors.requiredParams[param.key] = `${param.key} is required`;
      }
    });

    const paramKeys = new Set<string>();
    let duplicateKeyError = false;
    optionalParams.forEach(param => {
      const key = param.key.trim();
      if (key) {
        if (paramKeys.has(key)) {
          duplicateKeyError = true;
        }
        paramKeys.add(key);
      }
    });
    requiredParams.forEach(param => {
      const key = param.key.trim();
      if (key) {
        if (paramKeys.has(key)) {
        } else {
          paramKeys.add(key);
        }
      }
    });

    if (duplicateKeyError) {
      newErrors.optionalParams = "Duplicate optional parameter key detected";
    }

    setErrors(newErrors);
    const hasBaseErrors = !!newErrors.name || !!newErrors.selectedCombinedModel || !!newErrors.apiKey;
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

  const handleFetchModels = async () => {
    setIsFetchingModels(true);
    try {
      // If a configured provider is selected, fetch its models specifically
      if (selectedProvider?.source === 'configured') {
        const response = await getConfiguredProviderModels(selectedProvider.name, true);

        if (response.error || !response.data) {
          throw new Error(response.error || `Failed to fetch models for ${selectedProvider.name}`);
        }

        // Convert configured provider models to the expected format
        const providerKey = getProviderFormKey(selectedProvider.type as BackendModelProviderType);
        if (providerKey) {
          const models = response.data.models.map(modelName => ({
            name: modelName,
            function_calling: true // Assume function calling for configured providers
          }));

          setProviderModelsData(prev => ({
            ...prev,
            [providerKey]: models
          }));
        }

        toast.success(`Models fetched for ${selectedProvider.name}`);
      } else {
        // Fetch stock models
        const response = await getModels();

        if (response.error || !response.data) {
          throw new Error(response.error || "Failed to fetch models");
        }

        setProviderModelsData(response.data);
        toast.success("Models refreshed successfully");
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "Failed to fetch models";
      toast.error(errorMessage);
    } finally {
      setIsFetchingModels(false);
    }
  };

  const handleSubmit = async () => {
    if (!selectedCombinedModel) {
      setErrors(prev => ({...prev, selectedCombinedModel: "Provider and Model selection is required"}));
      toast.error("Please select a Provider and Model.");
      return;
    }

    const parts = selectedCombinedModel.split('::');
    if (parts.length !== 2 || !isValidProviderInfoKey(parts[0])) {
      toast.error("Invalid Provider/Model selection.");
      return;
    }
    const providerKey = parts[0] as ModelProviderKey;
    const modelName = parts[1];

    const finalSelectedProvider = providers.find(p => getProviderFormKey(p.type as BackendModelProviderType) === providerKey);

    if (!validateForm() || !finalSelectedProvider) {
      toast.error("Please fill in all required fields and correct any errors.");
      return;
    }
    setIsSubmitting(true);
    setErrors({});

    const finalApiKey = isApiKeyNeeded ? apiKey.trim() : "";

    let finalModelName = modelName;
    if (finalSelectedProvider.type === 'Ollama') {
      const tag = modelTag.trim();
      if (tag && tag !== OLLAMA_DEFAULT_TAG) {
        finalModelName = `${modelName}:${tag}`;
      }
    }

    const payload: CreateModelConfigRequest = {
      ref: k8sRefUtils.toRef(namespace, name),
      provider: {
        name: finalSelectedProvider.name,
        type: finalSelectedProvider.type,
      },
      model: finalModelName,
      apiKey: finalApiKey,
    };

    const providerParams = processModelParams(requiredParams, optionalParams);

    const providerType = finalSelectedProvider.type;
    switch (providerType) {
      case 'OpenAI':
        payload.openAI = providerParams as OpenAIConfigPayload;
        break;
      case 'Anthropic':
        payload.anthropic = providerParams as AnthropicConfigPayload;
        break;
      case 'AzureOpenAI':
        payload.azureOpenAI = providerParams as AzureOpenAIConfigPayload;
        break;
      case 'Ollama':
        payload.ollama = providerParams as OllamaConfigPayload;
        break;
      case 'Gemini':
        payload.gemini = providerParams as GeminiConfigPayload;
        break;
      case 'GeminiVertexAI':
        payload.geminiVertexAI = providerParams as GeminiVertexAIConfigPayload;
        break;
      case 'AnthropicVertexAI':
        payload.anthropicVertexAI = providerParams as AnthropicVertexAIConfigPayload;
        break;
      default:
        console.error("Unsupported provider type during payload construction:", providerType);
        toast.error("Internal error: Unsupported provider type.");
        setIsSubmitting(false);
        return;
    }

    try {
      let response;
      if (isEditMode && modelConfigName) {
        const updatePayload: UpdateModelConfigPayload = {
          provider: payload.provider,
          model: payload.model,
          apiKey: finalApiKey ? finalApiKey : null,
          openAI: payload.openAI,
          anthropic: payload.anthropic,
          azureOpenAI: payload.azureOpenAI,
          ollama: payload.ollama,
        };
        const modelConfigRef = k8sRefUtils.toRef(modelConfigNamespace || '', modelConfigName);
        response = await updateModelConfig(modelConfigRef, updatePayload);
      } else {
        response = await createModelConfig(payload);
      }

      if (!response.error) {
        toast.success(`Model configuration ${isEditMode ? 'updated' : 'created'} successfully!`);
        await refreshModels();
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

  if (isLoading && !isEditMode) {
    return <LoadingState />;
  }

  const showLoadingOverlay = isLoading && isEditMode;

  return (
    <div className="min-h-screen p-8 relative">
      {showLoadingOverlay && (
        <div className="absolute inset-0 bg-background/80 backdrop-blur-sm flex items-center justify-center z-50">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      )}

      <div className="max-w-6xl mx-auto">
        <h1 className="text-2xl font-bold mb-8">{isEditMode ? "Edit Model" : "Create New Model"}</h1>

        <div className="space-y-6">
          <BasicInfoSection
            name={name}
            isEditingName={isEditingName}
            namespace={namespace}
            errors={errors}
            isSubmitting={isSubmitting}
            isLoading={isLoading}
            onNameChange={setName}
            onToggleEditName={() => setIsEditingName(!isEditingName)}
            onNamespaceChange={setNamespace}
            providers={providers}
            providerModelsData={providerModelsData}
            selectedCombinedModel={selectedCombinedModel}
            onModelChange={(comboboxValue, providerKey, modelName, functionCalling) => {
              setSelectedCombinedModel(comboboxValue);
              setSelectedModelSupportsFunctionCalling(functionCalling);
              if (errors.selectedCombinedModel) {
                setErrors(prev => ({ ...prev, selectedCombinedModel: undefined }));
              }
            }}
            onProviderChange={(provider) => {
              setSelectedProvider(provider);

              // Clear models for this provider type when switching providers
              // This prevents showing wrong models when switching between providers with the same type
              // (e.g., stock OpenAI vs configured ai-gateway-openai)
              const providerKey = getProviderFormKey(provider.type as BackendModelProviderType);
              if (providerKey && providerModelsData?.[providerKey]) {
                setProviderModelsData(prev => {
                  if (!prev) return prev;
                  const newData = { ...prev };
                  delete newData[providerKey];
                  return newData;
                });
              }
            }}
            selectedProvider={selectedProvider}
            selectedModelSupportsFunctionCalling={selectedModelSupportsFunctionCalling}
            loadingError={loadingError}
            isEditMode={isEditMode}
            modelTag={modelTag}
            onModelTagChange={setModelTag}
            onFetchModels={handleFetchModels}
            isFetchingModels={isFetchingModels}
          />

          <AuthSection
            isOllamaSelected={isOllamaSelected}
            isEditMode={isEditMode}
            apiKey={apiKey}
            showApiKey={showApiKey}
            errors={errors}
            isSubmitting={isSubmitting}
            isLoading={isLoading}
            onApiKeyChange={setApiKey}
            onToggleShowApiKey={() => setShowApiKey(!showApiKey)}
            selectedProvider={selectedProvider}
            isApiKeyNeeded={isApiKeyNeeded}
            onApiKeyNeededChange={setIsApiKeyNeeded}
          />

          {selectedProvider && selectedCombinedModel && (
            <ParamsSection
              selectedProvider={selectedProvider}
              requiredParams={requiredParams}
              optionalParams={optionalParams}
              errors={errors}
              isSubmitting={isSubmitting}
              isLoading={isLoading}
              onRequiredParamChange={handleRequiredParamChange}
              onOptionalParamChange={handleOptionalParamChange}
              isExpanded={isParamsSectionExpanded}
              onToggleExpand={() => setIsParamsSectionExpanded(!isParamsSectionExpanded)}
              title="Custom parameters"
            />
          )}
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
  const readOnly = useReadOnly();
  const router = useRouter();

  useEffect(() => {
    if (readOnly) {
      router.replace("/");
    }
  }, [readOnly, router]);

  if (readOnly) {
    return null;
  }

  return (
    <React.Suspense fallback={<LoadingState />}>
      <ModelPageContent />
    </React.Suspense>
  );
}
