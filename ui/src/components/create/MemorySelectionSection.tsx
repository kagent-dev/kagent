"use client";

import * as React from "react";
import { Check, ChevronsUpDown, X } from "lucide-react";

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
import { Badge } from "@/components/ui/badge";
import { MemoryResponse } from "@/lib/types";

interface MemorySelectionSectionProps {
  availableMemories: MemoryResponse[];
  selectedMemories: string[];
  onSelectionChange: (selected: string[]) => void;
  disabled?: boolean;
  error?: string;
}

export function MemorySelectionSection({
  availableMemories,
  selectedMemories,
  onSelectionChange,
  disabled = false,
  error,
}: MemorySelectionSectionProps) {
  const [open, setOpen] = React.useState(false);
  const handleSelect = (memoryName: string) => {
    const newSelection = selectedMemories.includes(memoryName)
      ? selectedMemories.filter((name) => name !== memoryName)
      : [...selectedMemories, memoryName];
    onSelectionChange(newSelection);
  };

  const handleRemove = (memoryName: string) => {
    const newSelection = selectedMemories.filter((name) => name !== memoryName);
    onSelectionChange(newSelection);
  };

  return (
    <div className="space-y-2">
      <label className="text-sm mb-2 block">Select Memories</label>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={open}
            className={cn(
              "w-full justify-between h-auto min-h-[2.5rem] flex-wrap",
              error ? "border-red-500" : ""
            )}
            disabled={disabled}
          >
            <div className="flex flex-wrap gap-1">
              {selectedMemories.length === 0 && (
                <span className="text-muted-foreground">Select memories...</span>
              )}
              {selectedMemories.map((name) => (
                <Badge
                  key={name}
                  variant="secondary"
                  className="flex items-center gap-1 whitespace-nowrap"
                >
                  {name}
                  <X
                    className="h-3 w-3 cursor-pointer"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleRemove(name);
                    }}
                  />
                </Badge>
              ))}
            </div>
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
          <Command>
            <CommandInput placeholder="Search memories..." disabled={disabled} />
            <CommandList>
               <CommandEmpty>No memory found.</CommandEmpty>
              <CommandGroup>
                {availableMemories.map((memory) => (
                  <CommandItem
                    key={memory.name}
                    value={memory.name}
                    onSelect={() => {
                       handleSelect(memory.name);
                    }}
                    disabled={disabled}
                  >
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4",
                        selectedMemories.includes(memory.name)
                          ? "opacity-100"
                          : "opacity-0"
                      )}
                    />
                     {memory.name}
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {error && <p className="text-red-500 text-sm mt-1">{error}</p>}
       <p className="text-xs text-muted-foreground">
        Select memories for the agent to use.
      </p>
    </div>
  );
} 