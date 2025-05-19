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
  const getMemoryFullName = (memory: MemoryResponse) => `${memory.namespace}/${memory.name}`;
  const handleSelect = (memory: MemoryResponse) => {
    const memoryFullName = getMemoryFullName(memory);
    const newSelection = selectedMemories.includes(memoryFullName)
      ? selectedMemories.filter((name) => name !== memoryFullName)
      : [...selectedMemories, memoryFullName];
    onSelectionChange(newSelection);
  };

  const handleRemove = (memoryFullName: string) => {
    const newSelection = selectedMemories.filter((id) => id !== memoryFullName);
    onSelectionChange(newSelection);
  };

  return (
    <div className="space-y-2">
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
              {selectedMemories.map((memoryFullName) => (
                <Badge
                  key={memoryFullName}
                  variant="secondary"
                  className="flex items-center gap-1 whitespace-nowrap"
                >
                  {memoryFullName}
                  <X
                    className="h-3 w-3 cursor-pointer"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleRemove(memoryFullName);
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
                {availableMemories.map((memory) => {
                  const memoryFullName = getMemoryFullName(memory)
                  return (
                    <CommandItem
                      key={memoryFullName}
                      value={memoryFullName}
                      onSelect={() => {
                        handleSelect(memory);
                      }}
                      disabled={disabled}
                    >
                      <Check
                        className={cn(
                          "mr-2 h-4 w-4",
                          selectedMemories.includes(memoryFullName)
                            ? "opacity-100"
                            : "opacity-0"
                        )}
                      />
                      {memoryFullName}
                    </CommandItem>
                  )
                })}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {error && <p className="text-red-500 text-sm mt-1">{error}</p>}
    </div>
  );
}
