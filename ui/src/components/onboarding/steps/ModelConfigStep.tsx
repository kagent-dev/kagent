import React, { useState, useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import * as z from 'zod';
import { Button } from '@/components/ui/button';
import { CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Loader2, Info } from 'lucide-react';
import { toast } from 'sonner';
import type { CreateModelConfigRequest, ModelConfig, Provider, ProviderModelsResponse } from '@/types';
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { getModels } from '@/app/actions/models';
import { getSupportedModelProviders } from '@/app/actions/providers';
import { cn, isResourceNameValid, createRFC1123ValidName } from "@/lib/utils";
import { createModelConfig } from '@/app/actions/modelConfigs';
import { ModelProviderCombobox } from '@/components/ModelProviderCombobox';
import { PROVIDERS_INFO, isValidProviderInfoKey, modelProviders } from '@/lib/providers';
import { OLLAMA_DEFAULT_TAG, OLLAMA_DEFAULT_HOST } from '@/lib/constants';
import { k8sRefUtils } from '@/lib/k8sUtils';
import { K8S_AGENT_DEFAULTS } from '../OnboardingWizard';
import { NamespaceCombobox } from "@/components/NamespaceCombobox";

const modelConfigSchema = z.object({
    providerName: z.enum(modelProviders, { message: "Please select a provider." }),
    configName: z.string().min(1, "Configuration name is required."),
    configNamespace: z.string().optional(),
    modelName: z.string().min(1, "Model name is required."),
    apiKey: z.string().optional(),
    azureEndpoint: z.string().optional(),
    azureApiVersion: z.string().optional(),
    modelTag: z.string().optional(),
    ollamaBaseUrl: z.string().optional(),
}).refine(data => data.providerName === 'Ollama' || (data.apiKey && data.apiKey.length > 0), {
    message: "API Key is required for this provider.",
    path: ["apiKey"],
}).refine(data => data.providerName !== 'AzureOpenAI' || (data.azureEndpoint && data.azureEndpoint.length > 0), {
    message: "Azure Endpoint is required for Azure OpenAI.",
    path: ["azureEndpoint"],
}).refine(data => data.providerName !== 'AzureOpenAI' || (data.azureApiVersion && data.azureApiVersion.length > 0), {
    message: "Azure API Version is required for Azure OpenAI.",
    path: ["azureApiVersion"],
});
type ModelConfigFormData = z.infer<typeof modelConfigSchema>;

const selectModelSchema = z.object({
    selectedModelName: z.string().min(1, "Please select a model configuration.")
});
type SelectModelFormData = z.infer<typeof selectModelSchema>;

interface ModelConfigStepProps {
    existingModels: ModelConfig[] | null;
    loadingExistingModels: boolean;
    errorExistingModels: string | null;
    onNext: (modelConfigName: string, modelName: string) => void;
    onBack: () => void;
}

export function ModelConfigStep({
    existingModels,
    loadingExistingModels,
    errorExistingModels,
    onNext,
}: ModelConfigStepProps) {
    const [isLoading, setIsLoading] = useState(false);
    const [configMode, setConfigMode] = useState<'create' | 'select'>('create');
    const [providerModelsData, setProviderModelsData] = useState<ProviderModelsResponse | null>(null);
    const [providerModelsLoading, setProviderModelsLoading] = useState<boolean>(true);
    const [providerModelsError, setProviderModelsError] = useState<string | null>(null);
    const [supportedProviders, setSupportedProviders] = useState<Provider[]>([]);
    const [providersLoading, setProvidersLoading] = useState<boolean>(true);
    const [providersError, setProvidersError] = useState<string | null>(null);
    const [isOllama, setIsOllama] = useState(false);
    const [lastAutoGenName, setLastAutoGenName] = useState<string>("");

    useEffect(() => {
        if (!loadingExistingModels && existingModels && existingModels.length > 0) {
            setConfigMode('select');
        } else if (!loadingExistingModels) {
            setConfigMode('create');
        }
    }, [loadingExistingModels, existingModels]);

    useEffect(() => {
        const fetchProviderModels = async () => {
            setProviderModelsLoading(true);
            setProviderModelsError(null);
            try {
                const result = await getModels();
                if (!result.error && result.data) {
                    setProviderModelsData(result.data);
                } else {
                    throw new Error(result.error || 'Failed to fetch available models.');
                }
            } catch (error) {
                setProviderModelsError(error instanceof Error ? error.message : String(error));
                setProviderModelsData(null);
            } finally {
                setProviderModelsLoading(false);
            }
        };
        fetchProviderModels();
    }, []);

    useEffect(() => {
        const fetchProviders = async () => {
            setProvidersLoading(true);
            setProvidersError(null);
            try {
                const result = await getSupportedModelProviders();
                if (!result.error && result.data) {
                    setSupportedProviders(result.data);
                } else {
                    throw new Error(result.error || 'Failed to fetch supported providers.');
                }
            } catch (error) {
                console.error("Error fetching supported providers:", error);
                setProvidersError(error instanceof Error ? error.message : String(error));
                setSupportedProviders([]);
            } finally {
                setProvidersLoading(false);
            }
        };
        fetchProviders();
    }, []);

    const formStep1Create = useForm<ModelConfigFormData>({
        resolver: zodResolver(modelConfigSchema),
        defaultValues: {
            providerName: undefined, configName: "", configNamespace: "", modelName: "",
            apiKey: "", azureEndpoint: "", azureApiVersion: "", modelTag: "",
            ollamaBaseUrl: "",
        },
    });
    const formStep1Select = useForm<SelectModelFormData>({
        resolver: zodResolver(selectModelSchema),
        defaultValues: { selectedModelName: undefined }
    });

    const watchedProvider = formStep1Create.watch("providerName");
    const needsApiKey = watchedProvider && watchedProvider !== 'Ollama';
    const isAzure = watchedProvider === 'AzureOpenAI';
    const currentProviderName = formStep1Create.watch("providerName");
    const currentModelName = formStep1Create.watch("modelName");
    const currentCombinedValue = currentProviderName && currentModelName ? `${currentProviderName}::${currentModelName}` : "";

    const generateConfigName = (provider: string, model: string, tag?: string) => {
        if (!provider || !model) return "";

        const nameParts = [provider, model];
        if (provider === 'ollama' && tag && tag !== OLLAMA_DEFAULT_TAG) {
            nameParts.push(tag);
        }

        try {
            const proposedName = createRFC1123ValidName(nameParts);
            return proposedName && isResourceNameValid(proposedName) ? proposedName : "";
        } catch (e) {
            console.error("Error generating config name:", e);
            return "";
        }
    };

    useEffect(() => {
        setIsOllama(watchedProvider === 'Ollama');
    }, [watchedProvider]);

    async function onSubmitStep1Create(values: ModelConfigFormData) {
        setIsLoading(true);
        if (!isValidProviderInfoKey(values.providerName)) {
            toast.error("Invalid provider selected.");
            setIsLoading(false);
            return;
        }
        const providerInfo = PROVIDERS_INFO[values.providerName];
        const payload: CreateModelConfigRequest = {
            ref: k8sRefUtils.toRef(values.configNamespace || "", values.configName),
            provider: { name: providerInfo.name, type: providerInfo.type },
            model: values.modelName,
            apiKey: values.apiKey || "",
        };
        switch (values.providerName) {
            case 'AzureOpenAI':
                payload.azureOpenAI = { azureEndpoint: values.azureEndpoint || "", apiVersion: values.azureApiVersion || "" }; break;
            case 'OpenAI': payload.openAI = {}; break;
            case 'Anthropic': payload.anthropic = {}; break;
            case 'Gemini': payload.gemini = {}; break;
            case 'GeminiVertexAI': payload.geminiVertexAI = {}; break;
            case 'AnthropicVertexAI': payload.anthropicVertexAI = {}; break;
            case 'Ollama':
                const modelTag = values.modelTag?.trim() || "";
                if (modelTag && modelTag !== OLLAMA_DEFAULT_TAG) {
                    payload.model = `${values.modelName}:${modelTag}`;
                }
                payload.ollama = {
                    host: values.ollamaBaseUrl || "",
                };
            break;
        }

        try {
            const result = await createModelConfig(payload);
            if (!result.error) {
                const configRef = k8sRefUtils.toRef(values.configNamespace || K8S_AGENT_DEFAULTS.namespace, values.configName)
                toast.success(`Model configuration '${configRef}' created successfully!`);
                onNext(configRef, values.modelName); // Pass data to parent
            } else {
                throw new Error(result.error || 'Failed to create model configuration.');
            }
        } catch (error) {
            console.error("Error creating model config:", error);
            toast.error(error instanceof Error ? error.message : String(error));
        } finally {
            setIsLoading(false);
        }
    }

    function onSubmitStep1Select(values: SelectModelFormData) {
        const selectedModel = existingModels?.find(m => m.ref === values.selectedModelName);
        if (selectedModel) {
            onNext(selectedModel.ref, selectedModel.model); // Pass data to parent
        } else {
            toast.error("Selected model configuration not found. Please try again.");
        }
    }

    if (loadingExistingModels) return <LoadingState />;
    if (errorExistingModels) return <ErrorState message={`Failed to load configurations: ${errorExistingModels}`} />;

    const hasExistingModels = existingModels && existingModels.length > 0;

    return (
        <>
            <CardHeader className="pt-8 pb-4 border-b">
                <CardTitle className="text-2xl">Step 1: Configure AI Model</CardTitle>
                <CardDescription className="text-md">First, we need to connect to an LLM provider that will power our <span className="font-semibold">Kubernetes Assistant</span>.</CardDescription>
            </CardHeader>
            <CardContent className="px-8 pt-6 pb-6 space-y-6">
                {hasExistingModels && (
                    <>
                        <Alert variant="default" className="mb-4">
                            <Info className="h-4 w-4" />
                            <AlertTitle>Existing Configurations Found</AlertTitle>
                            <AlertDescription>
                                Awesome! Looks like you already have model configurations set up.
                                You can select one below or choose to create a new one.
                            </AlertDescription>
                        </Alert>
                        <RadioGroup
                            value={configMode}
                            onValueChange={(value: 'create' | 'select') => setConfigMode(value)}
                            className="mb-4 flex space-x-4"
                        >
                            <div className="flex items-center space-x-2">
                                <RadioGroupItem value="select" id="select" />
                                <Label htmlFor="select">Select Existing</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                                <RadioGroupItem value="create" id="create" />
                                <Label htmlFor="create">Create New</Label>
                            </div>
                        </RadioGroup>
                    </>
                )}

                {configMode === 'select' && hasExistingModels && (
                    <Form {...formStep1Select}>
                        <form onSubmit={formStep1Select.handleSubmit(onSubmitStep1Select)} className="space-y-6">
                            <FormField
                                control={formStep1Select.control}
                                name="selectedModelName"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>Select Configuration</FormLabel>
                                        <Select onValueChange={field.onChange} defaultValue={field.value}>
                                            <FormControl>
                                                <SelectTrigger>
                                                    <SelectValue placeholder="Choose an existing model configuration..." />
                                                </SelectTrigger>
                                            </FormControl>
                                            <SelectContent>
                                                {existingModels?.map(model => (
                                                    <SelectItem key={model.ref} value={model.ref}>
                                                        {model.ref} ({model.providerName}: {model.model})
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />
                            <Button type="submit" className="w-full" disabled={isLoading}>
                                {isLoading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                                Next: Agent Setup
                            </Button>
                        </form>
                    </Form>
                )}

                {configMode === 'create' && (
                    <Form {...formStep1Create}>
                        <form onSubmit={formStep1Create.handleSubmit(onSubmitStep1Create)} className="space-y-6">
                            {/* Provider & Model Combobox */}
                            <FormField
                                control={formStep1Create.control}
                                name="providerName"
                                render={() => (
                                    <FormItem className="flex flex-col">
                                        <FormLabel>Provider & Model</FormLabel>
                                        <ModelProviderCombobox
                                            providers={supportedProviders}
                                            models={providerModelsData}
                                            value={currentCombinedValue}
                                            onChange={(_, providerKey, modelName) => {
                                                formStep1Create.setValue('providerName', providerKey, { shouldValidate: true });
                                                formStep1Create.setValue('modelName', modelName, { shouldValidate: true });
                                                if (providerKey !== 'AzureOpenAI') {
                                                    formStep1Create.setValue('azureEndpoint', '');
                                                    formStep1Create.setValue('azureApiVersion', '');
                                                }
                                                const currentName = formStep1Create.getValues("configName");
                                                const currentTag = formStep1Create.getValues("modelTag");

                                                const newAutoName = generateConfigName(providerKey, modelName, currentTag);

                                                if (newAutoName && (!currentName || currentName === lastAutoGenName)) {
                                                    formStep1Create.setValue('configName', newAutoName, { shouldValidate: true });
                                                    setLastAutoGenName(newAutoName);
                                                }
                                            }}
                                            disabled={providersLoading || providerModelsLoading || isLoading}
                                            loading={providersLoading || providerModelsLoading}
                                            error={providersError || providerModelsError}
                                            filterFunctionCalling={true}
                                            placeholder="Select a model (supports function calling)..."
                                        />
                                        <FormDescription>
                                            Select the AI provider and model.
                                            {(providersError || providerModelsError) && <span className="text-destructive"> Error: {providersError || providerModelsError}</span>}
                                        </FormDescription>
                                        <FormMessage />
                                        {formStep1Create.formState.errors.modelName && !formStep1Create.formState.errors.providerName && (
                                            <p className={cn("text-sm font-medium text-destructive")}>
                                                {formStep1Create.formState.errors.modelName.message}
                                            </p>
                                        )}
                                    </FormItem>
                                )}/>

                            {/* Add the Ollama Base URL field after the Model Tag field for Ollama */}
                            {isOllama && (
                                <>
                                    {/* Model Tag Field for Ollama */}
                                    <FormField
                                        control={formStep1Create.control}
                                        name="modelTag"
                                        render={({ field }) => (
                                            <FormItem className="flex flex-col">
                                                <FormLabel>Model Tag</FormLabel>
                                                <FormControl>
                                                    <Input
                                                        placeholder={OLLAMA_DEFAULT_TAG}
                                                        {...field}
                                                        onChange={e => {
                                                            field.onChange(e);

                                                            if (watchedProvider === 'Ollama') {
                                                                const currentName = formStep1Create.getValues("configName");
                                                                const newTag = e.target.value.trim();

                                                                const newAutoName = generateConfigName(
                                                                    watchedProvider || "",
                                                                    currentModelName || "",
                                                                    newTag
                                                                );

                                                                if (newAutoName && (!currentName || currentName === lastAutoGenName)) {
                                                                    formStep1Create.setValue('configName', newAutoName, { shouldValidate: true });
                                                                    setLastAutoGenName(newAutoName);
                                                                }
                                                            }
                                                        }}
                                                    />
                                                </FormControl>
                                                <FormDescription>
                                                    Specify a tag for the Ollama model (e.g., latest, 7b, 13b)
                                                </FormDescription>
                                                <FormMessage />
                                            </FormItem>
                                        )}
                                    />

                                    {/* Ollama Base URL Field */}
                                    <FormField
                                        control={formStep1Create.control}
                                        name="ollamaBaseUrl"
                                        render={({ field }) => (
                                            <FormItem className="flex flex-col">
                                                <FormLabel>Ollama Base URL</FormLabel>
                                                <FormControl>
                                                    <Input
                                                        placeholder={OLLAMA_DEFAULT_HOST}
                                                        {...field}
                                                    />
                                                </FormControl>
                                                <FormDescription>
                                                    The base URL where your Ollama instance is running (default: {OLLAMA_DEFAULT_HOST})
                                                </FormDescription>
                                                <FormMessage />
                                            </FormItem>
                                        )}
                                    />
                                </>
                            )}

                            <FormField
                                control={formStep1Create.control}
                                name="configName"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>Configuration Name</FormLabel>
                                        <FormControl>
                                            <Input
                                                placeholder="e.g., My OpenAI Setup"
                                                {...field}
                                                onChange={e => {
                                                    field.onChange(e);
                                                    if (e.target.value !== lastAutoGenName) {}
                                                }}
                                            />
                                        </FormControl>
                                        <FormDescription>We picked a unique name, but feel free to change it!</FormDescription>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />

                            <FormField
                                control={formStep1Create.control}
                                name="configNamespace"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>Configuration Namespace</FormLabel>
                                        <FormControl>
                                            <NamespaceCombobox
                                                value={field.value || ""}
                                                onValueChange={field.onChange}
                                            />
                                        </FormControl>
                                        <FormDescription>A kubernetes namespace for your ModelConfig</FormDescription>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />

                            {isAzure && (
                                <>
                                    <FormField
                                        control={formStep1Create.control}
                                        name="azureEndpoint"
                                        render={({ field }) => (
                                        <FormItem>
                                            <FormLabel>Azure Endpoint</FormLabel>
                                            <FormControl>
                                            <Input type="url" placeholder="https://your-resource.openai.azure.com/" {...field} />
                                            </FormControl>
                                            <FormDescription>
                                            Your Azure OpenAI resource endpoint URL.
                                            {PROVIDERS_INFO['AzureOpenAI']?.apiKeyLink && (
                                                <> (<a href={PROVIDERS_INFO['AzureOpenAI'].apiKeyLink} target="_blank" rel="noopener noreferrer" className="underline">Find it here</a>)</>
                                            )}
                                            </FormDescription>
                                            <FormMessage />
                                        </FormItem>
                                        )}
                                    />
                                    <FormField
                                        control={formStep1Create.control}
                                        name="azureApiVersion"
                                        render={({ field }) => (
                                        <FormItem>
                                            <FormLabel>Azure API Version</FormLabel>
                                            <FormControl>
                                            <Input placeholder="e.g., 2024-02-01" {...field} />
                                            </FormControl>
                                            <FormDescription>
                                            The API version for your Azure OpenAI deployment (e.g., 2024-02-01).
                                            </FormDescription>
                                            <FormMessage />
                                        </FormItem>
                                        )}
                                    />
                                </>
                            )}

                            {needsApiKey && (
                                <FormField
                                    control={formStep1Create.control} name="apiKey"
                                    render={({ field }) => (
                                        <FormItem>
                                            <FormLabel>API Key</FormLabel>
                                            <FormControl><Input type="password" placeholder="Enter your API key" {...field} /></FormControl>
                                            <FormDescription>
                                                {watchedProvider && isValidProviderInfoKey(watchedProvider) && PROVIDERS_INFO[watchedProvider]?.help}
                                                {watchedProvider && isValidProviderInfoKey(watchedProvider) && PROVIDERS_INFO[watchedProvider]?.apiKeyLink && (
                                                    <> (<a href={PROVIDERS_INFO[watchedProvider].apiKeyLink} target="_blank" rel="noopener noreferrer" className="underline">Get Key</a>)</>
                                                )}
                                            </FormDescription>
                                            <FormMessage />
                                        </FormItem>
                                    )}
                                />
                            )}

                            {!needsApiKey && watchedProvider === 'Ollama' && isValidProviderInfoKey('Ollama') && (
                                <p className="text-sm text-muted-foreground">{PROVIDERS_INFO['Ollama']?.help}</p>
                            )}

                            <Button type="submit" disabled={isLoading} className="w-full">
                                {isLoading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                                Create & Continue
                            </Button>
                        </form>
                    </Form>
                )}
            </CardContent>
            <CardFooter className="flex justify-between items-center pb-8 pt-2">
                {/* No Back button for Step 1 */}
            </CardFooter>
        </>
    );
}
