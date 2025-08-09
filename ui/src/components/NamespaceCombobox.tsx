"use client";

import { useState, useEffect } from "react";
import { Check, ChevronDown, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { listNamespaces, type NamespaceResponse } from "@/app/actions/namespaces";

interface NamespaceComboboxProps {
  value?: string;
  onValueChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  // callback to handle errors in case the parent component wants to handle an error
  onError?: (error: string | null) => void;
}

export function NamespaceCombobox({
  value,
  onValueChange,
  placeholder = "Select namespace...",
  disabled = false,
  onError,
}: NamespaceComboboxProps) {
  const [open, setOpen] = useState(false);
  const [namespaces, setNamespaces] = useState<NamespaceResponse[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadNamespaces = async () => {
    try {
      setLoading(true);
      setError(null);
      const response = await listNamespaces();
      
      if (!response.error) {
        setNamespaces(response.data || []);
        setError(null);
        onError?.(null);
      } else {
        const errorMsg = response.error || 'Failed to load namespaces';
        setError(errorMsg);
        onError?.(errorMsg);
      }
    } catch (err) {
      console.error('Failed to load namespaces:', err);
      const errorMsg = err instanceof Error ? err.message : 'Failed to load namespaces';
      setError(errorMsg);
      onError?.(errorMsg);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadNamespaces();
  }, []);

  const selectedNamespace = namespaces.find((ns) => ns.name === value);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn(
            "w-full justify-between",
            error && "border-red-500 focus:border-red-500 focus:ring-red-500",
          )}
          disabled={disabled || loading}
        >
          {loading ? (
            <div className="flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              Loading namespaces...
            </div>
          ) : selectedNamespace ? (
            <div className="flex items-center gap-2">
              <span>{selectedNamespace.name}</span>
              <span className="text-xs text-muted-foreground">
                ({selectedNamespace.status})
              </span>
            </div>
          ) : (
            placeholder
          )}
          <ChevronDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-full p-0" align="start">
        <Command>
          <CommandInput placeholder="Search namespaces..." />
          <CommandList>
            {error ? (
              <div className="p-2 text-sm text-red-500">
                Error: {error}
              </div>
            ) : (
              <>
                <CommandEmpty>
                  {loading ? "Loading..." : "No namespaces found."}
                </CommandEmpty>
                <CommandGroup>
                  {namespaces.map((namespace) => (
                    <CommandItem
                      key={namespace.name}
                      value={namespace.name}
                      onSelect={(currentValue) => {
                        onValueChange(currentValue === value ? "" : currentValue);
                        setOpen(false);
                      }}
                    >
                      <Check
                        className={cn(
                          "mr-2 h-4 w-4",
                          value === namespace.name ? "opacity-100" : "opacity-0"
                        )}
                      />
                      <div className="flex flex-col">
                        <span>{namespace.name}</span>
                        <span className="text-xs text-muted-foreground">
                          Status: {namespace.status}
                        </span>
                      </div>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
} 