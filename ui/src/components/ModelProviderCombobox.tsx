import React, { useState, useMemo } from 'react';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Check, ChevronsUpDown } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Provider, ProviderModel, ProviderModelsResponse } from '@/types';
import { PROVIDERS_INFO, isValidProviderInfoKey, getApiKeyForProviderFormKey, ModelProviderKey } from '@/lib/providers';
import { OpenAI } from './icons/OpenAI';
import { Anthropic } from './icons/Anthropic';
import { Ollama } from './icons/Ollama';
import { Azure } from './icons/Azure';

interface ComboboxOption {
    label: string; // e.g., "OpenAI - gpt-4o"
    value: string; // e.g., "openai::gpt-4o"
    provider: ModelProviderKey; // e.g., "openai"
    modelName: string; // e.g., "gpt-4o"
}

interface ModelProviderComboboxProps {
    // supported providers from the backend
    providers: Provider[];
    // grouped models from the backend
    models: ProviderModelsResponse | null;
    // selected model (e.g. "openai::gpt-4o")
    value: string | undefined;
    // callback on selection
    onChange: (value: string, providerKey: ModelProviderKey, modelName: string, functionCalling: boolean) => void;
    disabled?: boolean;
    loading?: boolean;
    error?: string | null;
    // if true, only show models with function_calling: true
    filterFunctionCalling?: boolean;
    placeholder?: string;
    loadingPlaceholder?: string;
    errorPlaceholder?: string;
    emptyPlaceholder?: string;
}

export function ModelProviderCombobox({
    providers,
    models,
    value,
    onChange,
    disabled = false,
    loading = false,
    error = null,
    filterFunctionCalling = false,
    placeholder = "Select model...",
    loadingPlaceholder = "Loading models...",
    errorPlaceholder = "Error loading models",
    emptyPlaceholder = "No models available"
}: ModelProviderComboboxProps) {
    const [comboboxOpen, setComboboxOpen] = useState(false);

    const PROVIDER_ICONS: Record<ModelProviderKey, React.ComponentType<{ className?: string }>> = {
        'openai': OpenAI,
        'anthropic': Anthropic,
        'ollama': Ollama,
        'azure-openai': Azure,
    };

    const getProviderIcon = (providerKey: ModelProviderKey | undefined): React.ReactNode | null => {
        if (!providerKey || !PROVIDER_ICONS[providerKey]) {
            return null;
        }
        const IconComponent = PROVIDER_ICONS[providerKey];
        return <IconComponent className="h-4 w-4 mr-2 shrink-0" />;
    };

    const groupedProviderModelOptions = useMemo(() => {
        if (!providers || !models || providers.length === 0 || Object.keys(models).length === 0) {
            return {};
        }

        const groups: { [groupName: string]: ComboboxOption[] } = {};
        providers.forEach(provider => {
            let providerFormKey: ModelProviderKey;
            const providerNameLower = provider.name.toLowerCase();
            if (providerNameLower === 'azureopenai') {
                providerFormKey = 'azure-openai';
            } else if (isValidProviderInfoKey(providerNameLower)) {
                providerFormKey = providerNameLower;
            } else {
                console.warn(`Unsupported provider name found: ${provider.name}`);
                return;
            }

            if (!isValidProviderInfoKey(providerFormKey)) return;

            const actualModelKey = getApiKeyForProviderFormKey(providerFormKey);
            let providerModels: ProviderModel[] = [];

            if (actualModelKey && models[actualModelKey]) {
                providerModels = models[actualModelKey]
                    .filter(m => filterFunctionCalling ? m.function_calling === true : true);
            }

            const providerInfo = PROVIDERS_INFO[providerFormKey];
            const providerDisplayName = providerInfo?.name || provider.name;

            if (providerModels.length > 0) {
                if (!groups[providerDisplayName]) groups[providerDisplayName] = [];
                providerModels.forEach(model => {
                    groups[providerDisplayName].push({
                        label: `${providerDisplayName} - ${model.name}`,
                        value: `${providerFormKey}::${model.name}`,
                        provider: providerFormKey,
                        modelName: model.name
                    });
                });
                groups[providerDisplayName].sort((a, b) => a.modelName.localeCompare(b.modelName));
            }
        });
        const sortedGroupEntries = Object.entries(groups).sort(([keyA], [keyB]) => keyA.localeCompare(keyB));
        return Object.fromEntries(sortedGroupEntries);
    }, [providers, models, filterFunctionCalling]);

    const flatProviderModelOptions = useMemo(() => {
        return Object.values(groupedProviderModelOptions).flat();
    }, [groupedProviderModelOptions]);

    const currentSelection = useMemo(() => {
        return flatProviderModelOptions.find(option => option.value === value);
    }, [flatProviderModelOptions, value]);

    const triggerContent = useMemo(() => {
        if (loading) return loadingPlaceholder;
        if (error) return errorPlaceholder;
        if (currentSelection) {
            return (
                <>
                    <span className="mr-2">{getProviderIcon(currentSelection.provider)}</span>
                    {currentSelection.label}
                </>
            );
        }
        if (flatProviderModelOptions.length === 0 && !loading && !error) return emptyPlaceholder;
        return placeholder;
    }, [loading, error, currentSelection, flatProviderModelOptions.length, loadingPlaceholder, errorPlaceholder, emptyPlaceholder, placeholder]);

    return (
        <Popover open={comboboxOpen} onOpenChange={setComboboxOpen}>
            <PopoverTrigger asChild>
                <Button
                    variant="outline"
                    role="combobox"
                    aria-expanded={comboboxOpen}
                    className={cn(
                        "w-full justify-between",
                        !value && !loading && !error && "text-muted-foreground"
                    )}
                    disabled={disabled || loading || (flatProviderModelOptions.length === 0 && !loading) || !!error}
                >
                    <span className="flex items-center truncate">
                        {triggerContent}
                    </span>
                    <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
                </Button>
            </PopoverTrigger>
            <PopoverContent className="w-[--radix-popover-trigger-width] max-h-[--radix-popover-content-available-height] p-0">
                <Command filter={(itemValue: string, search: string) => {
                    const option = flatProviderModelOptions.find(opt => opt.value === itemValue);
                    if (option && option.label.toLowerCase().includes(search.toLowerCase())) return 1;
                    return 0;
                }}>
                    <CommandInput placeholder="Search provider or model..." />
                    <CommandList>
                        <CommandEmpty>No model found.</CommandEmpty>
                        {Object.entries(groupedProviderModelOptions).map(([groupName, options]) => (
                            <CommandGroup heading={groupName} key={groupName}>
                                {options.map((option) => (
                                    <CommandItem
                                        value={option.value}
                                        key={option.value}
                                        onSelect={(currentValue: string) => {
                                            const selectedOption = flatProviderModelOptions.find(opt => opt.value === currentValue);
                                            let modelDetail: ProviderModel | undefined = undefined;
                                            if (selectedOption && selectedOption.provider && models) {
                                                const apiKey = getApiKeyForProviderFormKey(selectedOption.provider);
                                                modelDetail = models[apiKey]?.find(m => m.name === selectedOption.modelName);
                                            }

                                            if (selectedOption) {
                                                const functionCalling = modelDetail?.function_calling ?? false;
                                                onChange(selectedOption.value, selectedOption.provider, selectedOption.modelName, functionCalling);
                                                setComboboxOpen(false);
                                            }
                                        }}
                                    >
                                        <Check className={cn("mr-2 h-4 w-4", value === option.value ? "opacity-100" : "opacity-0")} />
                                        {getProviderIcon(option.provider)}
                                        {option.modelName}
                                    </CommandItem>
                                ))}
                            </CommandGroup>
                        ))}
                    </CommandList>
                </Command>
            </PopoverContent>
        </Popover>
    );
} 