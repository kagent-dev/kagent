"use client";
import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { getModelConfig, createModelConfig, updateModelConfig } from "@/app/actions/modelConfigs";
import {
    CreateModelConfigPayload,
    UpdateModelConfigPayload,
    Provider,
    OpenAIConfigPayload,
    AzureOpenAIConfigPayload,
    AnthropicConfigPayload,
    OllamaConfigPayload
} from "@/lib/types";
import { toast } from "sonner";
import { isResourceNameValid, createRFC1123ValidName } from "@/lib/utils";
import { OLLAMA_DEFAULT_TAG } from "@/lib/constants"
import { getSupportedModelProviders } from "@/app/actions/providers";
import { getModels, ProviderModelsResponse } from "@/app/actions/models";
import { isValidProviderInfoKey, getProviderFormKey, ModelProviderKey, BackendModelProviderType } from "@/lib/providers";
import { BasicInfoSection } from '@/components/models/new/BasicInfoSection';
import { AuthSection } from '@/components/models/new/AuthSection';
import { ParamsSection } from '@/components/models/new/ParamsSection';
import { k8sRefUtils } from "@/lib/k8sUtils";

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

// Helper function to process parameters before submission

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
  const isOllamaSelected = selectedProvider?.type === "Ollama";

  useEffect(() => {
    let isMounted = true;
    const fetchData = async () => {
      setLoadingError(null);
      setIsLoading(true);
      try {
        const [providersResponse, modelsResponse] = await Promise.all([
          getSupportedModelProviders(),
          getModels()
        ]);

        if (!isMounted) return;

        if (providersResponse.success && providersResponse.data) {
          setProviders(providersResponse.data);
        } else {
          throw new Error(providersResponse.error || "Failed to fetch supported providers");
        }

        if (modelsResponse.success && modelsResponse.data) {
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
  }, []);

  useEffect(() => {
    let isMounted = true;
    const fetchModelData = async () => {
      if (isEditMode && modelConfigName && providers.length > 0 && providerModelsData) {
        try {
          if (!isLoading) setIsLoading(true);
          const response = await getModelConfig(
            k8sRefUtils.toRef(modelConfigNamespace || '', modelConfigName)
          );
          if (!isMounted) return;

          if (!response.success || !response.data) {
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
  }, [isEditMode, modelConfigName, providers, providerModelsData]);

  useEffect(() => {
    if (selectedProvider) {
      const requiredKeys = selectedProvider.requiredParams || [];
      const optionalKeys = selectedProvider.optionalParams || [];

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
  }, [selectedCombinedModel, isEditMode, isEditingName, modelTag]);

  useEffect(() => {
    if (!isApiKeyNeeded) {
      setApiKey("");
      if (errors.apiKey) {
        setErrors(prev => ({ ...prev, apiKey: undefined }));
      }
    }
  }, [isApiKeyNeeded]);

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

    const payload: CreateModelConfigPayload = {
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
              const prov = providers.find(p => getProviderFormKey(p.type as BackendModelProviderType) === providerKey);
              setSelectedProvider(prov || null);
              setSelectedModelSupportsFunctionCalling(functionCalling);
              if (errors.selectedCombinedModel) {
                setErrors(prev => ({ ...prev, selectedCombinedModel: undefined }));
              }
            }}
            selectedProvider={selectedProvider}
            selectedModelSupportsFunctionCalling={selectedModelSupportsFunctionCalling}
            loadingError={loadingError}
            isEditMode={isEditMode}
            modelTag={modelTag}
            onModelTagChange={setModelTag}
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
  return (
    <React.Suspense fallback={<LoadingState />}>
      <ModelPageContent />
    </React.Suspense>
  );
}
