"use client";

import { useState, useEffect } from "react";
import { Check, ChevronDown, Loader2, Network } from "lucide-react";
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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useSidebar } from "@/components/ui/sidebar";
import { listNamespaces, type NamespaceResponse } from "@/app/actions/namespaces";

interface NamespaceSelectorProps {
  value: string;
  onValueChange: (ns: string) => void;
}

export function NamespaceSelector({ value, onValueChange }: NamespaceSelectorProps) {
  const [open, setOpen] = useState(false);
  const [namespaces, setNamespaces] = useState<NamespaceResponse[]>([]);
  const [loading, setLoading] = useState(false);
  const { state } = useSidebar();
  const isCollapsed = state === "collapsed";

  useEffect(() => {
    const loadNamespaces = async () => {
      try {
        setLoading(true);
        const response = await listNamespaces();

        if (!response.error) {
          const sorted = [...(response.data || [])].sort((a, b) =>
            a.name.localeCompare(b.name, undefined, { sensitivity: "base" })
          );
          setNamespaces(sorted);

          // Set default namespace if none selected
          if (!value) {
            const names = sorted.map((ns) => ns.name);
            let defaultNamespace: string | undefined;
            if (names.includes("kagent")) {
              defaultNamespace = "kagent";
            } else if (names.includes("default")) {
              defaultNamespace = "default";
            } else if (names.length > 0) {
              defaultNamespace = names[0];
            }
            if (defaultNamespace) {
              onValueChange(defaultNamespace);
            }
          }
        }
      } catch (err) {
        console.error("Failed to load namespaces:", err);
      } finally {
        setLoading(false);
      }
    };

    loadNamespaces();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (isCollapsed) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 shrink-0"
              aria-label={`Namespace: ${value || "none"}`}
            >
              {loading ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Network className="h-4 w-4" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent side="right">
            {value || "No namespace"}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between h-8 px-2 text-xs"
          disabled={loading}
        >
          {loading ? (
            <div className="flex items-center gap-2">
              <Loader2 className="h-3 w-3 animate-spin" />
              <span>Loading...</span>
            </div>
          ) : (
            <div className="flex items-center gap-2 truncate">
              <Network className="h-3 w-3 shrink-0" />
              <span className="truncate">{value || "Select namespace..."}</span>
            </div>
          )}
          <ChevronDown className="ml-1 h-3 w-3 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0" align="start" side="right">
        <Command>
          <CommandInput placeholder="Search namespaces..." />
          <CommandList>
            <CommandEmpty>No namespaces found.</CommandEmpty>
            <CommandGroup>
              {namespaces.map((ns) => (
                <CommandItem
                  key={ns.name}
                  value={ns.name}
                  onSelect={(selected) => {
                    onValueChange(selected === value ? "" : selected);
                    setOpen(false);
                  }}
                >
                  <Check
                    className={cn(
                      "mr-2 h-4 w-4",
                      value === ns.name ? "opacity-100" : "opacity-0"
                    )}
                  />
                  <span>{ns.name}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
