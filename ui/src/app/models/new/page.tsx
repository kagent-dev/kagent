"use client";

import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Loader2, Brain, Sparkles, Building, Shield, BarChart3, Globe, Server, Database, Cloud, Network, Bot, Cpu, Key, Lock, Eye, EyeOff, Settings, Zap, CheckCircle, AlertCircle } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { getModelConfig, createModelConfig, updateModelConfig } from "@/app/actions/modelConfigs";
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
import { getSupportedModelProviders } from "@/app/actions/providers";
import { getModels } from "@/app/actions/models";
import { isValidProviderInfoKey, getProviderFormKey, ModelProviderKey, BackendModelProviderType } from "@/lib/providers";
import { BasicInfoSection } from '@/components/models/new/BasicInfoSection';
import { AuthSection } from '@/components/models/new/AuthSection';
import { ParamsSection } from '@/components/models/new/ParamsSection';
import { k8sRefUtils } from "@/lib/k8sUtils";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";

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
        if (!providersResponse.error && providersResponse.data) {
          setProviders(providersResponse.data);
        } else {
          throw new Error(providersResponse.error || "Failed to fetch supported providers");
        }

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
          if (!isLoading) setIsLoading(true);
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
  }, [isEditMode, modelConfigName, providers, providerModelsData, isLoading, modelConfigNamespace]);

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
  }, [selectedCombinedModel, isEditMode, isEditingName, modelTag, selectedProvider]);

  useEffect(() => {
    if (!isApiKeyNeeded) {
      setApiKey("");
      if (errors.apiKey) {
        setErrors(prev => ({ ...prev, apiKey: undefined }));
      }
    }
  }, [isApiKeyNeeded, errors.apiKey]);

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

  const getProviderIcon = (providerType: string) => {
    const type = providerType.toLowerCase();
    if (type.includes('openai')) return <Brain className="w-6 h-6 text-green-600" />;
    if (type.includes('anthropic')) return <Sparkles className="w-6 h-6 text-orange-600" />;
    if (type.includes('google') || type.includes('gemini')) return <Globe className="w-6 h-6 text-blue-600" />;
    if (type.includes('azure')) return <Cloud className="w-6 h-6 text-blue-500" />;
    if (type.includes('aws')) return <Server className="w-6 h-6 text-orange-500" />;
    if (type.includes('ollama')) return <Cpu className="w-6 h-6 text-purple-600" />;
    return <Bot className="w-6 h-6 text-slate-600" />;
  };

  if (error) {
    return <ErrorState message={error} />;
  }

  if (isLoading && !isEditMode) {
    return <LoadingState />;
  }

  const showLoadingOverlay = isLoading && isEditMode;

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 via-blue-50 to-indigo-50 relative overflow-hidden">
      {/* Enterprise Background Pattern */}
      <div className="absolute inset-0 opacity-5">
        <div className="absolute top-20 left-20 w-32 h-32 bg-blue-500 rounded-full blur-3xl"></div>
        <div className="absolute top-40 right-32 w-24 h-24 bg-indigo-500 rounded-full blur-3xl"></div>
        <div className="absolute bottom-32 left-1/3 w-20 h-20 bg-slate-500 rounded-full blur-3xl"></div>
        <div className="absolute bottom-20 right-20 w-28 h-28 bg-purple-500 rounded-full blur-3xl"></div>
      </div>

      {/* Loading Overlay */}
      {showLoadingOverlay && (
        <div className="absolute inset-0 bg-white/80 backdrop-blur-xl flex items-center justify-center z-50">
          <Card className="border-0 bg-white/95 backdrop-blur-xl shadow-2xl">
            <CardContent className="flex flex-col items-center justify-center p-12">
              <div className="w-16 h-16 rounded-3xl bg-gradient-to-br from-blue-600 via-indigo-600 to-slate-600 flex items-center justify-center mb-6">
                <Brain className="w-8 h-8 text-white animate-pulse" />
              </div>
              <h3 className="text-xl font-bold text-slate-900 mb-2">Loading Model Configuration</h3>
              <p className="text-slate-600">Retrieving your AI model settings...</p>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Enterprise Header */}
      <div className="relative z-10">
        <div className="max-w-7xl mx-auto px-8 py-12">
          {/* Page Header */}
          <div className="text-center mb-12">
            <div className="flex items-center justify-center gap-4 mb-6">
              <div className="w-16 h-16 rounded-3xl bg-gradient-to-br from-blue-600 via-indigo-600 to-slate-600 flex items-center justify-center shadow-2xl">
                <Brain className="w-8 h-8 text-white" />
              </div>
              <div>
                <h1 className="text-4xl font-bold bg-gradient-to-r from-slate-900 via-blue-900 to-indigo-900 bg-clip-text text-transparent mb-2">
                  {isEditMode ? "Edit AI Model" : "Create AI Model"}
                </h1>
                <p className="text-lg text-slate-600 max-w-2xl">
                  {isEditMode 
                    ? "Modify your enterprise AI model configuration with advanced intelligence settings"
                    : "Configure powerful AI models for your enterprise applications with advanced intelligence and security"
                  }
                </p>
              </div>
            </div>

            {/* Enterprise Features Grid */}
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 max-w-4xl mx-auto">
              <div className="bg-gradient-to-br from-blue-50 to-indigo-50 p-4 rounded-2xl border border-blue-100 shadow-sm">
                <Brain className="w-8 h-8 text-blue-600 mb-2 mx-auto" />
                <div className="text-sm font-semibold text-blue-900">Advanced AI</div>
              </div>
              <div className="bg-gradient-to-br from-indigo-50 to-purple-50 p-4 rounded-2xl border border-indigo-100 shadow-sm">
                <Shield className="w-8 h-8 text-indigo-600 mb-2 mx-auto" />
                <div className="text-sm font-semibold text-indigo-900">Enterprise Security</div>
              </div>
              <div className="bg-gradient-to-br from-slate-50 to-gray-50 p-4 rounded-2xl border border-slate-100 shadow-sm">
                <Network className="w-8 h-8 text-slate-600 mb-2 mx-auto" />
                <div className="text-sm font-semibold text-slate-900">Multi-Provider</div>
              </div>
              <div className="bg-gradient-to-br from-green-50 to-emerald-50 p-4 rounded-2xl border border-green-100 shadow-sm">
                <BarChart3 className="w-8 h-8 text-green-600 mb-2 mx-auto" />
                <div className="text-sm font-semibold text-green-900">Performance Analytics</div>
              </div>
            </div>
          </div>

          {/* Main Form */}
          <div className="max-w-6xl mx-auto space-y-8">
            {/* Basic Information Card */}
            <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
              <CardHeader className="bg-gradient-to-r from-blue-600 via-indigo-600 to-slate-600 text-white">
                <CardTitle className="flex items-center gap-3 text-2xl">
                  <Building className="w-6 h-6" />
                  Basic Configuration
                </CardTitle>
                <p className="text-blue-100">Configure the fundamental properties of your AI model</p>
              </CardHeader>
              <CardContent className="p-8">
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
              </CardContent>
            </Card>

            {/* Authentication Card */}
            <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
              <CardHeader className="bg-gradient-to-r from-green-600 via-emerald-600 to-teal-600 text-white">
                <CardTitle className="flex items-center gap-3 text-2xl">
                  <Key className="w-6 h-6" />
                  Authentication & Security
                </CardTitle>
                <p className="text-green-100">Configure secure access credentials for your AI model</p>
              </CardHeader>
              <CardContent className="p-8">
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
              </CardContent>
            </Card>

            {/* Advanced Parameters Card */}
            {selectedProvider && selectedCombinedModel && (
              <Card className="bg-white/95 backdrop-blur-xl border-0 shadow-2xl overflow-hidden">
                <CardHeader className="bg-gradient-to-r from-purple-600 via-pink-600 to-rose-600 text-white">
                  <CardTitle className="flex items-center gap-3 text-2xl">
                    <Settings className="w-6 h-6" />
                    Advanced Parameters
                  </CardTitle>
                  <p className="text-purple-100">Fine-tune your AI model with custom parameters and configurations</p>
                </CardHeader>
                <CardContent className="p-8">
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
                </CardContent>
              </Card>
            )}

            {/* Action Buttons */}
            <div className="flex justify-center pt-8">
              <div className="flex gap-4">
                <Button 
                  variant="outline" 
                  onClick={() => router.push('/models')}
                  className="h-14 px-8 text-lg border-2 border-slate-300 hover:border-slate-400"
                  disabled={isSubmitting}
                >
                  Cancel
                </Button>
                <Button 
                  className="h-14 px-12 text-lg bg-gradient-to-r from-blue-600 via-indigo-600 to-slate-600 hover:from-blue-700 hover:via-indigo-700 hover:to-slate-700 shadow-xl hover:shadow-2xl transition-all duration-300"
                  onClick={handleSubmit} 
                  disabled={isSubmitting || isLoading}
                >
                  {isSubmitting ? (
                    <>
                      <Loader2 className="h-5 w-5 mr-3 animate-spin" />
                      {isEditMode ? "Updating Model..." : "Creating Model..."}
                    </>
                  ) : isEditMode ? (
                    <>
                      <Brain className="h-5 w-5 mr-3" />
                      Update AI Model
                    </>
                  ) : (
                    <>
                      <Sparkles className="h-5 w-5 mr-3" />
                      Create AI Model
                    </>
                  )}
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Enterprise Footer */}
      <div className="relative z-10 bg-white/90 backdrop-blur-xl border-t border-slate-200 mt-12">
        <div className="max-w-7xl mx-auto px-8 py-8">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-8">
            <div className="text-center">
              <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-blue-600 to-indigo-600 flex items-center justify-center mx-auto mb-4">
                <Brain className="w-6 h-6 text-white" />
              </div>
              <h3 className="font-bold text-lg mb-2">AI Intelligence</h3>
              <p className="text-slate-600 text-sm">Advanced machine learning models powering your applications</p>
            </div>
            <div className="text-center">
              <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-green-600 to-emerald-600 flex items-center justify-center mx-auto mb-4">
                <Shield className="w-6 h-6 text-white" />
              </div>
              <h3 className="font-bold text-lg mb-2">Enterprise Security</h3>
              <p className="text-slate-600 text-sm">Bank-grade security with end-to-end encryption</p>
            </div>
            <div className="text-center">
              <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-purple-600 to-pink-600 flex items-center justify-center mx-auto mb-4">
                <BarChart3 className="w-6 h-6 text-white" />
              </div>
              <h3 className="font-bold text-lg mb-2">Performance Analytics</h3>
              <p className="text-slate-600 text-sm">Real-time monitoring and optimization</p>
            </div>
          </div>
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
