import React, { useState, useMemo } from 'react';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { Check, ChevronsUpDown } from 'lucide-react';
import { cn } from '@/lib/utils';
import { ProviderModel } from '@/types';

interface ModelComboboxProps {
  models: ProviderModel[];
  value: string | undefined;
  onChange: (modelName: string, functionCalling: boolean) => void;
  disabled?: boolean;
  placeholder?: string;
  emptyMessage?: string;
}

export function ModelCombobox({
  models,
  value,
  onChange,
  disabled = false,
  placeholder = "Select model...",
  emptyMessage = "No models available"
}: ModelComboboxProps) {
  const [open, setOpen] = useState(false);

  const sortedModels = useMemo(() => {
    return [...models].sort((a, b) => a.name.localeCompare(b.name));
  }, [models]);

  const selectedModel = useMemo(() => {
    return sortedModels.find(m => m.name === value);
  }, [sortedModels, value]);

  const triggerContent = useMemo(() => {
    if (selectedModel) {
      return selectedModel.name;
    }
    if (sortedModels.length === 0 && !disabled) return emptyMessage;
    return placeholder;
  }, [selectedModel, sortedModels.length, disabled, emptyMessage, placeholder]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn(
            "w-full justify-between",
            !value && "text-muted-foreground"
          )}
          disabled={disabled || sortedModels.length === 0}
        >
          <span className="flex items-center truncate">
            {triggerContent}
          </span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder="Search models..." />
          <CommandList>
            <CommandEmpty>No model found.</CommandEmpty>
            {sortedModels.map((model) => (
              <CommandItem
                key={model.name}
                value={model.name}
                onSelect={() => {
                  onChange(model.name, model.function_calling ?? false);
                  setOpen(false);
                }}
              >
                <Check
                  className={cn(
                    "mr-2 h-4 w-4",
                    value === model.name ? "opacity-100" : "opacity-0"
                  )}
                />
                {model.name}
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
