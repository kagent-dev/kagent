import React, { useState, useMemo } from 'react';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList, CommandSeparator } from "@/components/ui/command";
import { Check, ChevronsUpDown } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Provider } from '@/types';
import { ModelProviderKey } from '@/lib/providers';
import { OpenAI } from './icons/OpenAI';
import { Anthropic } from './icons/Anthropic';
import { Ollama } from './icons/Ollama';
import { Azure } from './icons/Azure';
import { Gemini } from './icons/Gemini';

const PROVIDER_ICONS: Record<ModelProviderKey, React.ComponentType<{ className?: string }>> = {
  'OpenAI': OpenAI,
  'Anthropic': Anthropic,
  'Ollama': Ollama,
  'AzureOpenAI': Azure,
  'Gemini': Gemini,
  'GeminiVertexAI': Gemini,
  'AnthropicVertexAI': Anthropic,
};

function getProviderIcon(providerType: string | undefined): React.ReactNode | null {
  if (!providerType || !(providerType in PROVIDER_ICONS)) {
    return null;
  }
  const IconComponent = PROVIDER_ICONS[providerType as ModelProviderKey];
  return <IconComponent className="h-4 w-4 mr-2 shrink-0" />;
}

interface ProviderComboboxProps {
  providers: Provider[];
  value: Provider | null;
  onChange: (provider: Provider) => void;
  disabled?: boolean;
  loading?: boolean;
}

export function ProviderCombobox({
  providers,
  value,
  onChange,
  disabled = false,
  loading = false,
}: ProviderComboboxProps) {
  const [open, setOpen] = useState(false);

  const groupedProviders = useMemo(() => {
    const stock = providers.filter(p => p.source === 'stock' || !p.source).sort((a, b) => a.name.localeCompare(b.name));
    const configured = providers.filter(p => p.source === 'configured').sort((a, b) => a.name.localeCompare(b.name));
    return { stock, configured };
  }, [providers]);

  const hasProviders = groupedProviders.stock.length > 0 || groupedProviders.configured.length > 0;

  const triggerContent = useMemo(() => {
    if (loading) return "Loading providers...";
    if (value) {
      return (
        <>
          {getProviderIcon(value.type)}
          {value.name}
        </>
      );
    }
    if (!hasProviders) return "No providers available";
    return "Select provider...";
  }, [loading, value, hasProviders]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn(
            "w-full justify-between",
            !value && !loading && "text-muted-foreground"
          )}
          disabled={disabled || loading || !hasProviders}
        >
          <span className="flex items-center truncate">
            {triggerContent}
          </span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder="Search providers..." />
          <CommandList>
            <CommandEmpty>No provider found.</CommandEmpty>

            {/* Configured Providers (shown first) */}
            {groupedProviders.configured.length > 0 && (
              <CommandGroup heading="Configured Providers">
                {groupedProviders.configured.map((provider) => (
                  <CommandItem
                    key={`configured-${provider.name}`}
                    value={`configured-${provider.name}`}
                    onSelect={() => {
                      onChange(provider);
                      setOpen(false);
                    }}
                  >
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4",
                        value?.name === provider.name && value?.source === provider.source ? "opacity-100" : "opacity-0"
                      )}
                    />
                    {getProviderIcon(provider.type)}
                    {provider.name}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {/* Separator if both groups exist */}
            {groupedProviders.configured.length > 0 && groupedProviders.stock.length > 0 && (
              <CommandSeparator />
            )}

            {/* Stock Providers */}
            {groupedProviders.stock.length > 0 && (
              <CommandGroup heading="Stock Providers">
                {groupedProviders.stock.map((provider) => (
                  <CommandItem
                    key={`stock-${provider.type}`}
                    value={`stock-${provider.name}`}
                    onSelect={() => {
                      onChange(provider);
                      setOpen(false);
                    }}
                  >
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4",
                        value?.name === provider.name && value?.source === provider.source ? "opacity-100" : "opacity-0"
                      )}
                    />
                    {getProviderIcon(provider.type)}
                    {provider.name}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
